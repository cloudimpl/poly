package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/smithy-go"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/urfave/cli/v2"
)

var ecrAccounts = []string{"537413656254", "485496110001"}

//go:embed resources/docker-compose-env.yml
var DockerComposeEnv string

//go:embed resources/docker-compose-platform.yml
var DockerComposePlatform string

//go:embed resources/entrypoint.sh
var EntrypointScript string

//go:embed resources/Dockerfile
var Dockerfile string

func hasBuildx() bool {
	out, err := exec.Command("docker", "buildx", "version").CombinedOutput()
	return err == nil && len(out) > 0
}

func getPolycodeDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Errorf("failed to get user home directory: %w", err))
	}

	polycodeDir := filepath.Join(homeDir, ".polycode")

	// Create .polycode directory if it doesn't exist
	if err := os.MkdirAll(polycodeDir, 0755); err != nil {
		panic(fmt.Errorf("failed to create .polycode directory: %w", err))
	}

	return polycodeDir
}

func ensurePolycodeDirAndCopyFiles() error {
	polycodeDir := getPolycodeDir()

	// Define the files to write
	files := map[string]string{
		"docker-compose-env.yml":      DockerComposeEnv,
		"docker-compose-platform.yml": DockerComposePlatform,
		"entrypoint.sh":               EntrypointScript,
		"Dockerfile":                  Dockerfile,
	}

	for name, content := range files {
		targetPath := filepath.Join(polycodeDir, name)

		// Only write if the file doesn't already exist
		if _, err := os.Stat(targetPath); os.IsNotExist(err) {
			if err := os.WriteFile(targetPath, []byte(content), 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", name, err)
			}
		}
	}

	return nil
}

func syncSidecarFromS3() error {
	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg)
	downloader := manager.NewDownloader(s3Client)

	bucket := "buildspecs.polycode.app"
	objectPrefix := "polycode/engine/"

	polycodeDir := getPolycodeDir()
	runtimeDir := filepath.Join(polycodeDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}

	localChecksumPath := filepath.Join(runtimeDir, "sidecar.checksum")
	sidecarPath := filepath.Join(runtimeDir, "sidecar")
	remoteChecksumPath := filepath.Join(runtimeDir, "latest.checksum.tmp")

	// === Download remote checksum ===
	remoteChecksumFile, err := os.Create(remoteChecksumPath)
	if err != nil {
		return fmt.Errorf("failed to create temp checksum file: %w", err)
	}
	defer remoteChecksumFile.Close()

	_, err = downloader.Download(ctx, remoteChecksumFile, &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    awsStr(objectPrefix + "latest.checksum"),
	})
	if err != nil {
		return fmt.Errorf("failed to download remote checksum: %w", err)
	}

	// === Read checksums ===
	remoteChecksumBytes, _ := os.ReadFile(remoteChecksumPath)
	remoteChecksum := strings.TrimSpace(string(remoteChecksumBytes))

	var localChecksum string
	if data, err := os.ReadFile(localChecksumPath); err == nil {
		localChecksum = strings.TrimSpace(string(data))
	}

	fmt.Println("Local Checksum :", localChecksum)
	fmt.Println("Remote Checksum:", remoteChecksum)

	if localChecksum != remoteChecksum {
		fmt.Println("Checksum differs or missing, downloading sidecar...")

		sidecarFile, err := os.Create(sidecarPath)
		if err != nil {
			return fmt.Errorf("failed to create sidecar file: %w", err)
		}
		defer sidecarFile.Close()

		_, err = downloader.Download(ctx, sidecarFile, &s3.GetObjectInput{
			Bucket: &bucket,
			Key:    awsStr(objectPrefix + "latest"),
		})
		if err != nil {
			return fmt.Errorf("failed to download sidecar binary: %w", err)
		}

		// Make executable
		if err := os.Chmod(sidecarPath, 0755); err != nil {
			return fmt.Errorf("failed to chmod sidecar binary: %w", err)
		}

		// Overwrite local checksum file
		if err := os.WriteFile(localChecksumPath, []byte(remoteChecksum), 0644); err != nil {
			return fmt.Errorf("failed to update checksum file: %w", err)
		}

		fmt.Println("Sidecar updated.")
	} else {
		fmt.Println("Sidecar is up to date.")
	}

	_ = os.Remove(remoteChecksumPath)
	return nil
}

func awsStr(s string) *string {
	return &s
}

func startPlatform() error {
	if err := ensurePolycodeDirAndCopyFiles(); err != nil {
		return fmt.Errorf("copy files: %w", err)
	}
	if err := syncSidecarFromS3(); err != nil {
		return fmt.Errorf("sync sidecar: %w", err)
	}

	checkCmd := exec.Command("docker", "compose",
		"-f", "docker-compose-platform.yml",
		"-p", "polycode-platform",
		"ps", "--format", "json")
	checkCmd.Dir = getPolycodeDir()

	output, err := checkCmd.Output()
	if err == nil && len(output) > 0 {
		// Parse JSON to check if any service is "running"
		var services []struct {
			Name  string `json:"Name"`
			State string `json:"State"`
		}
		if err := json.Unmarshal(output, &services); err == nil {
			for _, svc := range services {
				if svc.State == "running" {
					fmt.Println("✅ Platform already started.")
					return nil
				}
			}
		}
	}

	fmt.Println("Starting platform")

	cmd := exec.Command("docker", "compose",
		"-f", "docker-compose-platform.yml",
		"-p", "polycode-platform",
		"up", "-d")

	// Set working directory to ~/.polycode
	cmd.Dir = getPolycodeDir()

	// Execute the command
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose failed: %w", err)
	}

	time.Sleep(3 * time.Second)
	fmt.Println("✅ Platform started.")
	return nil
}

func stopPlatform() error {
	fmt.Println("Stopping platform")

	cmd := exec.Command("docker", "compose",
		"-f", "docker-compose-platform.yml",
		"-p", "polycode-platform",
		"down")

	// Set working directory to ~/.polycode
	cmd.Dir = getPolycodeDir()

	// Execute the command
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose failed: %w", err)
	}

	time.Sleep(3 * time.Second)
	fmt.Println("🛑 Platform stopped.")
	return nil
}

func psPlatform() error {
	fmt.Println("Checking platform status...")

	cmd := exec.Command("docker", "compose",
		"-f", "docker-compose-platform.yml",
		"-p", "polycode-platform",
		"ps", "--format", "json")

	cmd.Dir = getPolycodeDir()

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose ps failed: %w", err)
	}

	var containers []struct {
		Name  string `json:"Name"`
		State string `json:"State"` // e.g., "running", "exited"
	}

	if err := json.Unmarshal(out.Bytes(), &containers); err != nil {
		return fmt.Errorf("failed to parse docker compose ps output: %w", err)
	}

	if len(containers) == 0 {
		fmt.Println("STOPPED")
		return nil
	}

	allRunning := true
	anyExited := false

	for _, c := range containers {
		state := strings.ToLower(c.State)
		switch state {
		case "running":
			// good
		case "exited", "dead", "removing":
			anyExited = true
			allRunning = false
		default:
			allRunning = false
		}
	}

	if allRunning {
		fmt.Println("RUNNING")
	} else if anyExited {
		fmt.Println("ERROR")
	} else {
		fmt.Println("STOPPED")
	}

	return nil
}

func attr(name string, t types.ScalarAttributeType) types.AttributeDefinition {
	return types.AttributeDefinition{AttributeName: aws.String(name), AttributeType: t}
}

func key(name string, keyType types.KeyType) types.KeySchemaElement {
	return types.KeySchemaElement{AttributeName: aws.String(name), KeyType: keyType}
}

func ensureBucket(ctx context.Context, s3client *s3.Client, bucketName string) error {
	_, err := s3client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err == nil {
		return nil
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NotFound":
			// Try to create the bucket
			_, err := s3client.CreateBucket(ctx, &s3.CreateBucketInput{
				Bucket: aws.String(bucketName),
			})
			if err != nil {
				var createErr smithy.APIError
				if errors.As(err, &createErr) && createErr.ErrorCode() == "BucketAlreadyOwnedByYou" {
					return nil
				}
				return fmt.Errorf("failed to create bucket: %w", err)
			}
			return nil
		case "Forbidden":
			return nil
		default:
			return fmt.Errorf("unexpected API error on HeadBucket: %w", err)
		}
	}

	return fmt.Errorf("failed to check or create bucket: %w", err)
}

func setupPlatform(ctx context.Context) error {
	dummyCredentials := aws.NewCredentialsCache(
		credentials.NewStaticCredentialsProvider(
			"minioadmin",
			"minioadmin",
			"",
		),
	)

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(dummyCredentials),
	)
	if err != nil {
		return fmt.Errorf("failed to load dev mode aws config: %w", err)
	}

	ddb := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String("http://localhost:8000/")
	})

	tables := []struct {
		Name   string
		Schema *dynamodb.CreateTableInput
	}{
		{
			"polycode-workflows",
			&dynamodb.CreateTableInput{
				TableName:   aws.String("polycode-workflows"),
				BillingMode: types.BillingModePayPerRequest,
				AttributeDefinitions: []types.AttributeDefinition{
					attr("PKEY", types.ScalarAttributeTypeS),
					attr("RKEY", types.ScalarAttributeTypeS),
					attr("AppId", types.ScalarAttributeTypeS),
					attr("EndTime", types.ScalarAttributeTypeN),
					attr("InstanceId", types.ScalarAttributeTypeS),
					attr("Timestamp", types.ScalarAttributeTypeN),
					attr("TraceId", types.ScalarAttributeTypeS),
				},
				KeySchema: []types.KeySchemaElement{
					key("PKEY", types.KeyTypeHash),
					key("RKEY", types.KeyTypeRange),
				},
				GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
					{
						IndexName: aws.String("AppId-Timestamp-index"),
						KeySchema: []types.KeySchemaElement{
							key("AppId", types.KeyTypeHash),
							key("Timestamp", types.KeyTypeRange),
						},
						Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
					},
					{
						IndexName: aws.String("AppId-EndTime-index"),
						KeySchema: []types.KeySchemaElement{
							key("AppId", types.KeyTypeHash),
							key("EndTime", types.KeyTypeRange),
						},
						Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
					},
					{
						IndexName: aws.String("InstanceId-EndTime-index"),
						KeySchema: []types.KeySchemaElement{
							key("InstanceId", types.KeyTypeHash),
							key("EndTime", types.KeyTypeRange),
						},
						Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
					},
					{
						IndexName: aws.String("InstanceId-Timestamp-index"),
						KeySchema: []types.KeySchemaElement{
							key("InstanceId", types.KeyTypeHash),
							key("Timestamp", types.KeyTypeRange),
						},
						Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
					},
					{
						IndexName: aws.String("TraceId-Timestamp-index"),
						KeySchema: []types.KeySchemaElement{
							key("TraceId", types.KeyTypeHash),
							key("Timestamp", types.KeyTypeRange),
						},
						Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
					},
				},
			},
		},
		{
			"polycode-logs",
			&dynamodb.CreateTableInput{
				TableName:   aws.String("polycode-logs"),
				BillingMode: types.BillingModePayPerRequest,
				AttributeDefinitions: []types.AttributeDefinition{
					attr("PKEY", types.ScalarAttributeTypeS),
					attr("RKEY", types.ScalarAttributeTypeN),
					attr("AppId", types.ScalarAttributeTypeS),
				},
				KeySchema: []types.KeySchemaElement{
					key("PKEY", types.KeyTypeHash),
					key("RKEY", types.KeyTypeRange),
				},
				GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
					{
						IndexName: aws.String("AppId-RKEY-index"),
						KeySchema: []types.KeySchemaElement{
							key("AppId", types.KeyTypeHash),
							key("RKEY", types.KeyTypeRange),
						},
						Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
					},
				},
			},
		},
		{
			"polycode-data",
			&dynamodb.CreateTableInput{
				TableName:   aws.String("polycode-data"),
				BillingMode: types.BillingModePayPerRequest,
				AttributeDefinitions: []types.AttributeDefinition{
					attr("PKEY", types.ScalarAttributeTypeS),
					attr("RKEY", types.ScalarAttributeTypeS),
				},
				KeySchema: []types.KeySchemaElement{
					key("PKEY", types.KeyTypeHash),
					key("RKEY", types.KeyTypeRange),
				},
			},
		},
		{
			"polycode-meta",
			&dynamodb.CreateTableInput{
				TableName:   aws.String("polycode-meta"),
				BillingMode: types.BillingModePayPerRequest,
				AttributeDefinitions: []types.AttributeDefinition{
					attr("PKEY", types.ScalarAttributeTypeS),
					attr("RKEY", types.ScalarAttributeTypeS),
				},
				KeySchema: []types.KeySchemaElement{
					key("PKEY", types.KeyTypeHash),
					key("RKEY", types.KeyTypeRange),
				},
			},
		},
	}

	for _, t := range tables {
		_, err := ddb.DescribeTable(ctx, &dynamodb.DescribeTableInput{
			TableName: aws.String(t.Name),
		})
		if err == nil {
			continue
		}
		if _, err := ddb.CreateTable(ctx, t.Schema); err != nil {
			return fmt.Errorf("failed to create table %s: %w", t.Name, err)
		}
	}

	// Set up MinIO S3 bucket
	s3client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String("http://localhost:9000/")
		o.UsePathStyle = true
	})
	bucketName := "polycode-files"

	err = ensureBucket(ctx, s3client, bucketName)
	if err != nil {
		return err
	}

	fmt.Println("🚀 Platform ready!")
	return nil
}

func loginDockerRegistries() error {
	ctx := context.Background()

	// === Load AWS Config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	// === Get ECR auth token
	ecrClient := ecr.NewFromConfig(cfg)
	authOutput, err := ecrClient.GetAuthorizationToken(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to get ECR auth token: %w", err)
	}
	if len(authOutput.AuthorizationData) == 0 {
		return fmt.Errorf("no authorization data from ECR")
	}

	authData := authOutput.AuthorizationData[0]
	decoded, err := base64.StdEncoding.DecodeString(*authData.AuthorizationToken)
	if err != nil {
		return fmt.Errorf("failed to decode auth token: %w", err)
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid authorization token format")
	}
	password := parts[1]

	for _, account := range ecrAccounts {
		registry := fmt.Sprintf("%s.dkr.ecr.us-east-1.amazonaws.com", account)
		cmd := exec.Command("docker", "login", "--username", "AWS", "--password-stdin", registry)
		cmd.Stdin = strings.NewReader(password)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("docker login failed for %s: %w", registry, err)
		}
	}

	return nil
}

func startEnvironment(envID string) error {
	if envID == "" {
		return fmt.Errorf("environment ID is required")
	}

	checkCmd := exec.Command("docker", "compose",
		"-f", "docker-compose-env.yml",
		"-p", "polycode-env-"+envID,
		"ps", "--format", "json")
	checkCmd.Dir = getPolycodeDir()

	output, err := checkCmd.Output()
	if err == nil && len(output) > 0 {
		// Parse JSON to check if any service is "running"
		var services []struct {
			Name  string `json:"Name"`
			State string `json:"State"`
		}
		if err := json.Unmarshal(output, &services); err == nil {
			for _, svc := range services {
				if svc.State == "running" {
					fmt.Println("✅ Environment already started.")
					return nil
				}
			}
		}
	}

	err = loginDockerRegistries()
	if err != nil {
		return err
	}

	fmt.Println("Starting environment...")

	cmd := exec.Command("docker", "compose",
		"-f", "docker-compose-env.yml",
		"-p", "polycode-env-"+envID,
		"up", "-d")

	// Set working directory to ~/.polycode
	cmd.Dir = getPolycodeDir()

	cmd.Env = append(os.Environ(), "ENVIRONMENT_ID="+envID) // ✅ set ENVIRONMENT_ID for docker-compose

	// Execute the command
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose failed: %w", err)
	}

	time.Sleep(3 * time.Second)
	fmt.Printf("🚀 Environment %s ready!\n", envID)
	return nil
}

func stopEnvironment(envID string) error {
	if envID == "" {
		return fmt.Errorf("environment ID is required")
	}

	fmt.Println("Stopping environment...")

	cmd := exec.Command("docker", "compose",
		"-f", "docker-compose-env.yml",
		"-p", "polycode-env-"+envID,
		"down")

	// Set working directory to ~/.polycode
	cmd.Dir = getPolycodeDir()

	cmd.Env = append(os.Environ(), "ENVIRONMENT_ID="+envID) // ✅ set ENVIRONMENT_ID for docker-compose

	// Execute the command
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose failed: %w", err)
	}

	time.Sleep(3 * time.Second)
	fmt.Println("🛑 Environment stopped.")
	return nil
}

func psEnvironment(envID string) error {
	if envID == "" {
		return fmt.Errorf("environment ID is required")
	}

	fmt.Println("Checking environment status...")

	cmd := exec.Command("docker", "compose",
		"-f", "docker-compose-env.yml",
		"-p", "polycode-env-"+envID,
		"ps", "--format", "json")

	cmd.Dir = getPolycodeDir()
	cmd.Env = append(os.Environ(), "ENVIRONMENT_ID="+envID)

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose ps failed: %w", err)
	}

	var containers []struct {
		Name  string `json:"Name"`
		State string `json:"State"` // e.g., "running", "exited", etc.
	}

	if err := json.Unmarshal(out.Bytes(), &containers); err != nil {
		return fmt.Errorf("failed to parse docker compose ps output: %w", err)
	}

	if len(containers) == 0 {
		fmt.Println("STOPPED")
		return nil
	}

	allRunning := true
	anyExited := false

	for _, c := range containers {
		state := strings.ToLower(c.State)
		switch state {
		case "running":
			// OK
		case "exited", "dead", "removing":
			anyExited = true
			allRunning = false
		default:
			allRunning = false
		}
	}

	if allRunning {
		fmt.Println("RUNNING")
	} else if anyExited {
		fmt.Println("ERROR")
	} else {
		fmt.Println("STOPPED")
	}

	return nil
}

func cleanPlatform() error {
	dirPath := getPolycodeDir() // e.g., ~/.polycode
	err := os.RemoveAll(dirPath)
	if err != nil {
		return err
	}

	fmt.Println("✅ Cleaned .polycode directory")
	return nil
}

func runApp(appPath string, envID string, hostPort string) error {
	absAppPath, err := filepath.Abs(appPath)
	if err != nil {
		return fmt.Errorf("invalid app path: %w", err)
	}

	if stat, err := os.Stat(absAppPath); err != nil || !stat.IsDir() {
		return fmt.Errorf("app folder '%s' does not exist", absAppPath)
	}

	appName := filepath.Base(absAppPath)
	projectRoot, err := getGitRoot(absAppPath)
	if err != nil {
		return fmt.Errorf("detecting git root: %w", err)
	}

	serviceIDs, _ := findServiceIDs(filepath.Join(absAppPath, "services"))

	appFolder := "."
	if absAppPath != projectRoot {
		appFolder = appName
	}

	imageTag := fmt.Sprintf("%s:latest", appName)
	fmt.Println("🛠️  Building image:", imageTag)
	err = dockerBuild(projectRoot, appFolder, imageTag)
	if err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	// Build docker run command
	runArgs := []string{
		"run", "--rm", "-it",
		"--network", "polycode-dev",
		"-p", "2345:2345",
		"-v", fmt.Sprintf("%s:/project", projectRoot),
		"-e", "polycode_DEV_MODE=true",
		"-e", "polycode_ORG_ID=xxx",
		"-e", fmt.Sprintf("polycode_ENV_ID=%s", envID),
		"-e", fmt.Sprintf("polycode_APP_NAME=%s", appName),
		"-e", fmt.Sprintf("polycode_SERVICE_IDS=%s", serviceIDs),
	}

	if hostPort != "" {
		runArgs = append(runArgs, "-p", fmt.Sprintf("%s:8080", hostPort))
	}

	runArgs = append(runArgs, imageTag)

	fmt.Println("🚀 Running container...")
	cmd := exec.Command("docker", runArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func getGitRoot(path string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = path
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to detect git root: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func findServiceIDs(path string) (string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", nil // okay if no services
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return strings.Join(names, ","), nil
}

func dockerBuild(contextDir, appFolder, imageTag string) error {
	if !hasBuildx() {
		return fmt.Errorf("docker buildx is not installed")
	}

	dockerfilePath := filepath.Join(getPolycodeDir(), "Dockerfile")

	cmd := exec.Command(
		"docker", "build",
		"--load",
		"--build-arg", fmt.Sprintf("APP_FOLDER=%s", appFolder),
		"--build-context", fmt.Sprintf("platform=%s", getPolycodeDir()),
		"-t", imageTag,
		"-f", dockerfilePath, // explicitly set Dockerfile path
		".", // set build context
	)
	cmd.Dir = contextDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func main() {
	app := &cli.App{
		Name:  "polycode",
		Usage: "Manage the local Polycode platform",
		Commands: []*cli.Command{
			{
				Name:  "platform",
				Usage: "Manage the core platform (Docker + local stack)",
				Subcommands: []*cli.Command{
					{
						Name:  "start",
						Usage: "Start the platform (Docker, DynamoDB, S3)",
						Action: func(c *cli.Context) error {
							if err := startPlatform(); err != nil {
								return fmt.Errorf("start docker: %w", err)
							}
							if err := setupPlatform(context.Background()); err != nil {
								return fmt.Errorf("setup platform: %w", err)
							}
							return nil
						},
					},
					{
						Name:  "stop",
						Usage: "Stop the platform",
						Action: func(c *cli.Context) error {
							if err := stopPlatform(); err != nil {
								return fmt.Errorf("stop docker: %w", err)
							}
							return nil
						},
					},
					{
						Name:  "status",
						Usage: "View the status of the platform",
						Action: func(c *cli.Context) error {
							if err := psPlatform(); err != nil {
								return fmt.Errorf("stop docker: %w", err)
							}
							return nil
						},
					},
					{
						Name:  "clean",
						Usage: "Clean the platform",
						Action: func(c *cli.Context) error {
							if err := stopPlatform(); err != nil {
								return fmt.Errorf("stop docker: %w", err)
							}

							if err := cleanPlatform(); err != nil {
								return fmt.Errorf("stop command: %w", err)
							}
							return nil
						},
					},
				},
			},
			{
				Name:  "environment",
				Usage: "Manage project environments",
				Subcommands: []*cli.Command{
					{
						Name:      "start",
						Usage:     "Start an environment",
						ArgsUsage: "<environment-id>",
						Action: func(c *cli.Context) error {
							if c.Args().Len() < 1 {
								return fmt.Errorf("missing <environment-id>")
							}
							envID := c.Args().Get(0)

							if err := startEnvironment(envID); err != nil {
								return fmt.Errorf("stop docker: %w", err)
							}
							return nil
						},
					},
					{
						Name:      "stop",
						Usage:     "Stop an environment",
						ArgsUsage: "<environment-id>",
						Action: func(c *cli.Context) error {
							if c.Args().Len() < 1 {
								return fmt.Errorf("missing <environment-id>")
							}
							envID := c.Args().Get(0)

							if err := stopEnvironment(envID); err != nil {
								return fmt.Errorf("stop docker: %w", err)
							}
							return nil
						},
					},
					{
						Name:      "status",
						Usage:     "View the status of an environment",
						ArgsUsage: "<environment-id>",
						Action: func(c *cli.Context) error {
							if c.Args().Len() < 1 {
								return fmt.Errorf("missing <environment-id>")
							}
							envID := c.Args().Get(0)

							if err := psEnvironment(envID); err != nil {
								return fmt.Errorf("stop docker: %w", err)
							}
							return nil
						},
					},
				},
			},
			{
				Name:      "run",
				Usage:     "Run an app in the given environment",
				ArgsUsage: "<environment-id> [host-port]",
				Action: func(c *cli.Context) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("missing <environment-id>")
					}

					envID := c.Args().Get(0)
					hostPort := ""
					if c.Args().Len() >= 2 {
						hostPort = c.Args().Get(1)
					}

					appPath, err := os.Getwd()
					if err != nil {
						return fmt.Errorf("failed to get current directory: %w", err)
					}

					if err := runApp(appPath, envID, hostPort); err != nil {
						return fmt.Errorf("run app failed: %w", err)
					}

					return nil
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
