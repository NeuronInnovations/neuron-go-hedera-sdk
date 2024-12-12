// environment.go
//
// This file is responsible for managing the application's environment variables.
// It loads environment data from a combination of sources:
// - A `.env` file specified by the `--envFile` flag (defaulting to `.env` if not provided).
// - Command-line flags passed as arguments (see flags.go for details).
//
// The `InitEnv` function ensures that the environment variables are initialized correctly,
// either from the specified `.env` file or directly from the operating system's environment
// if the file is not found. This approach supports flexibility for deployment scenarios like
// local development or Kubernetes, where environment variables may already be present in the OS.
//
// Additionally, this file provides functionality for dynamically updating environment variables
// (`UpdateEnvVariable`) and ensures that any changes are safely written back to the `.env` file
// while creating a backup to avoid accidental data loss.
//
// Key responsibilities:
// - Load environment data from the `.env` file and system environment.
// - Provide methods for updating and persisting environment variables.
// - Handle file operations like backup creation and restoration to ensure robustness.

package commonlib

import (
	"crypto"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/libp2p/go-libp2p/core/protocol"
	flag "github.com/spf13/pflag"

	"github.com/hashgraph/hedera-sdk-go/v2"
	"github.com/joho/godotenv"
)

type EnvironmentVarLocation struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lon"`
	Altitude  float64 `json:"alt"`
	GPSFix    string  `json:"gpsfix"`
}

var (
	MyEnvFile    string
	MyProtocol   protocol.ID
	MyStdIn      hedera.TopicID
	MyStdOut     hedera.TopicID
	MyStdErr     hedera.TopicID
	MyPublicKey  hedera.PublicKey
	MyPrivateKey crypto.PrivateKey
	MyLocation   EnvironmentVarLocation

	// MyArbiterPublicKey  hedera.PublicKey
)

func InitEnv() {
	envFile := flag.String("envFile", ".env", "Location of .env file to override the default one")
	flag.CommandLine.ParseErrorsWhitelist.UnknownFlags = true
	flag.Parse()
	MyEnvFile = *envFile
	if e := godotenv.Load(*envFile); e != nil {
		log.Println("The supplied ", *envFile, " was not found...; This is bad but we'll continue anyway; maybe you're on kubernetes and keys are present in the os env ...")
		//TODO: check if environment data is present in the os
	}
}

func UpdateEnvVariable(key, value string, envFile string) error {
	if envFile == "" {
		envFile = ".env"
	}
	// Load the .env file into a map
	envMap, err := godotenv.Read(envFile)
	if err != nil {
		log.Println("Error loading .env file")
		return err
	}
	envMap[key] = value
	os.Setenv(key, value)
	err = writeEnvFile(envFile, envMap)
	if err != nil {
		fmt.Println("Error writing .env file")
		return err
	}

	return nil
}

func writeEnvFile(filename string, envMap map[string]string) error {
	// Create a backup of the original file
	backupFilename := filename + ".bak"
	err := copyFile(filename, backupFilename)
	if err != nil {
		return fmt.Errorf("failed to create backup: %v", err)
	}

	// Write the new content to the file
	var lines []string
	for key, value := range envMap {
		lines = append(lines, key+"="+value)
	}
	sort.Strings(lines)
	content := strings.Join(lines, "\n")

	err = os.WriteFile(filename, []byte(content), 0644)
	if err != nil {
		// Restore from backup if writing fails
		restoreErr := restoreBackup(filename, backupFilename)
		if restoreErr != nil {
			return fmt.Errorf("failed to write and restore backup: %v, restore error: %v", err, restoreErr)
		}
		return fmt.Errorf("failed to write file, restored backup: %v", err)
	}

	// Remove the backup after successful write
	err = os.Remove(backupFilename)
	if err != nil {
		return fmt.Errorf("file written but failed to remove backup: %v", err)
	}

	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	return nil
}

// restoreBackup restores the original file from the backup
func restoreBackup(originalFile, backupFile string) error {
	return copyFile(backupFile, originalFile)
}
