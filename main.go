package main

import (
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
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
			fmt.Println("Created:", targetPath)
		} else {
			fmt.Println("Exists, skipping:", targetPath)
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

	fmt.Println("Starting platform")

	cmd := exec.Command("docker", "compose",
		"-f", "docker-compose-platform.yml",
		"-p", "polycode-platform",
		"up", "-d")

	// Set working directory to ~/.polycode
	cmd.Dir = getPolycodeDir()

	// Optional: stream output to terminal
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

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

	// Optional: stream output to terminal
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	// Execute the command
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose failed: %w", err)
	}

	time.Sleep(3 * time.Second)
	fmt.Println("🛑 Platform stopped.")
	return nil
}

func attr(name string, t types.ScalarAttributeType) types.AttributeDefinition {
	return types.AttributeDefinition{AttributeName: aws.String(name), AttributeType: t}
}

func key(name string, keyType types.KeyType) types.KeySchemaElement {
	return types.KeySchemaElement{AttributeName: aws.String(name), KeyType: keyType}
}

func setupPlatform(ctx context.Context) error {
	dummyCredentials := aws.NewCredentialsCache(
		credentials.NewStaticCredentialsProvider(
			"minioadmin",
			"minioadmin",
			"minioadmin",
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
			fmt.Println("Table exists:", t.Name)
			continue
		}
		fmt.Println("Creating table:", t.Name)
		if _, err := ddb.CreateTable(ctx, t.Schema); err != nil {
			return fmt.Errorf("failed to create table %s: %w", t.Name, err)
		}
		fmt.Println("Created:", t.Name)
	}

	// Set up MinIO S3 bucket
	s3client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String("http://localhost:9000/")
	})
	bucketName := "polycode-files"

	_, err = s3client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucketName)})
	if err == nil {
		fmt.Println("S3 bucket exists:", bucketName)
	} else {
		fmt.Println("Creating S3 bucket:", bucketName)
		_, err := s3client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucketName)})
		if err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
		fmt.Println("Created bucket:", bucketName)
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

	fmt.Println("Logging into ECR for region us-east-1")
	for _, account := range ecrAccounts {
		registry := fmt.Sprintf("%s.dkr.ecr.us-east-1.amazonaws.com", account)
		cmd := exec.Command("docker", "login", "--username", "AWS", "--password-stdin", registry)
		cmd.Stdin = strings.NewReader(password)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("docker login failed for %s: %w", registry, err)
		}
		fmt.Printf("✅ Logged into %s\n", registry)
	}

	return nil
}

func startEnvironment(envID string) error {
	if envID == "" {
		return fmt.Errorf("environment ID is required")
	}

	err := loginDockerRegistries()
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

	// Optional: stream output to terminal
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

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

	// Optional: stream output to terminal
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	time.Sleep(3 * time.Second)
	fmt.Println("🛑 Environment stopped.")
	return nil
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
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
