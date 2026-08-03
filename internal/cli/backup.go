package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/simpleflo/conduit/internal/config"
)

// backupCmd creates a backup of Conduit data
func backupCmd() *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Create a backup of Conduit data",
		Long: `Create a complete backup of the Conduit data directory.

The backup includes:
  - Database (conduit.db)
  - Configuration (conduit.yaml)
  - Knowledge base data
  - Connector configurations

The backup is saved as a compressed tar.gz archive.

Examples:
  conduit backup
  conduit backup --output ~/backups/conduit-backup.tar.gz`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Ensure backups directory exists
			if err := os.MkdirAll(cfg.BackupsDir(), 0700); err != nil {
				return fmt.Errorf("create backups directory: %w", err)
			}

			// Determine output path
			if outputPath == "" {
				timestamp := time.Now().Format("20060102-150405")
				outputPath = filepath.Join(cfg.BackupsDir(), fmt.Sprintf("conduit-backup-%s.tar.gz", timestamp))
			}

			// Resolve to absolute path
			absOutput, err := filepath.Abs(outputPath)
			if err != nil {
				return fmt.Errorf("resolve output path: %w", err)
			}

			fmt.Printf("Creating backup of %s\n", cfg.DataDir)
			fmt.Printf("Output: %s\n", absOutput)
			fmt.Println()

			// Create the backup using tar
			fmt.Println("📦 Backing up data directory...")

			// Create output file
			outFile, err := os.Create(absOutput)
			if err != nil {
				return fmt.Errorf("create backup file: %w", err)
			}
			defer outFile.Close()

			// Use tar command for simplicity and better compatibility
			tarCmd := exec.Command("tar", "-czf", "-", "-C", filepath.Dir(cfg.DataDir), filepath.Base(cfg.DataDir))
			tarCmd.Stdout = outFile

			var stderr bytes.Buffer
			tarCmd.Stderr = &stderr

			if err := tarCmd.Run(); err != nil {
				return fmt.Errorf("create archive: %w (%s)", err, stderr.String())
			}

			// Get file size
			info, _ := os.Stat(absOutput)
			fmt.Printf("\n✓ Backup complete: %s (%s)\n", absOutput, formatBytes(info.Size()))

			// Show what was backed up
			fmt.Println("\nContents:")
			listCmd := exec.Command("tar", "-tzf", absOutput)
			listOut, _ := listCmd.Output()
			lines := strings.Split(string(listOut), "\n")
			shown := 0
			for _, line := range lines {
				if line != "" && shown < 10 {
					fmt.Printf("  %s\n", line)
					shown++
				}
			}
			if len(lines) > 10 {
				fmt.Printf("  ... and %d more files\n", len(lines)-10)
			}

			fmt.Println("\nTo restore, extract the archive to ~/.conduit")

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output path for backup file")

	return cmd
}
