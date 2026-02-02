package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

// FileEntry represents a file from the DMDE listing
type FileEntry struct {
	Path string
	Size int64
}

// DMDEData holds all parsed data from a DMDE file
type DMDEData struct {
	Directories       []string
	Files             []FileEntry
	ExpectedDirCount  int
	ExpectedFileCount int
}

func main() {
	if len(os.Args) < 4 {
		printUsage()
		os.Exit(1)
	}

	mode := os.Args[1]
	dmdeFile := os.Args[2]
	targetDir := os.Args[3]

	// Validate mode flag
	isCreate := mode == "-c" || mode == "--create"
	isVerify := mode == "-v" || mode == "--verify"

	if !isCreate && !isVerify {
		fmt.Printf("Error: Invalid mode '%s'. Must be -c/--create or -v/--verify\n\n", mode)
		printUsage()
		os.Exit(1)
	}

	// Parse the DMDE file
	data, err := parseDMDEFile(dmdeFile)
	if err != nil {
		fmt.Printf("Error parsing DMDE file: %v\n", err)
		os.Exit(1)
	}

	if isCreate {
		runCreateMode(data, targetDir)
	} else {
		runVerifyMode(data, targetDir)
	}
}

func printUsage() {
	fmt.Println("Usage: dirparser <mode> <dmde_file> <target_directory>")
	fmt.Println()
	fmt.Println("Modes:")
	fmt.Println("  -c, --create    Create directory structure from DMDE file listing")
	fmt.Println("  -v, --verify    Verify recovered files against DMDE file listing")
	fmt.Println()
	fmt.Println("Arguments:")
	fmt.Println("  <dmde_file>         Path to the DMDE file listing")
	fmt.Println("  <target_directory>  Directory to create structure in / verify against")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  dirparser -c filelist.txt ./recovered")
	fmt.Println("  dirparser --verify filelist.txt ./recovered")
}

func runCreateMode(data *DMDEData, outputDir string) {
	fmt.Printf("Found %d directories in the DMDE file\n", len(data.Directories))

	// Create the directories
	createdCount, skippedCount, err := createDirectories(data.Directories, outputDir)
	if err != nil {
		fmt.Printf("Error creating directories: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nSummary: %d created, %d already existed\n", createdCount, skippedCount)

	// Verify the count
	totalProcessed := createdCount + skippedCount
	if data.ExpectedDirCount > 0 {
		if totalProcessed == data.ExpectedDirCount {
			fmt.Printf("✓ Verification successful: Processed %d directories (matches expected count)\n", totalProcessed)
		} else {
			fmt.Printf("✗ Verification failed: Processed %d directories, but expected %d\n", totalProcessed, data.ExpectedDirCount)
			os.Exit(1)
		}
	} else {
		fmt.Println("Warning: Could not find 'Total directories:' in the file for verification")
	}
}

func runVerifyMode(data *DMDEData, targetDir string) {
	fmt.Printf("Verifying against DMDE listing: %d directories, %d files\n", len(data.Directories), len(data.Files))
	fmt.Println()

	hasErrors := false
	hasWarnings := false

	// Track all expected paths (for detecting extra files)
	expectedPaths := make(map[string]bool)
	for _, dir := range data.Directories {
		normalizedPath := filepath.FromSlash(strings.ReplaceAll(dir, "\\", "/"))
		fullPath := filepath.Join(targetDir, normalizedPath)
		expectedPaths[strings.ToLower(fullPath)] = true
	}
	for _, file := range data.Files {
		normalizedPath := filepath.FromSlash(strings.ReplaceAll(file.Path, "\\", "/"))
		fullPath := filepath.Join(targetDir, normalizedPath)
		expectedPaths[strings.ToLower(fullPath)] = true
	}

	// Verify directories
	fmt.Println("=== Checking Directories ===")
	missingDirs := 0
	existingDirs := 0
	for _, dir := range data.Directories {
		normalizedPath := filepath.FromSlash(strings.ReplaceAll(dir, "\\", "/"))
		fullPath := filepath.Join(targetDir, normalizedPath)

		if info, err := os.Stat(fullPath); err != nil {
			fmt.Printf("  ✗ MISSING DIR:  %s\n", fullPath)
			missingDirs++
			hasErrors = true
		} else if !info.IsDir() {
			fmt.Printf("  ✗ NOT A DIR:    %s (exists as file)\n", fullPath)
			missingDirs++
			hasErrors = true
		} else {
			existingDirs++
		}
	}
	fmt.Printf("  Directories: %d OK, %d missing\n", existingDirs, missingDirs)

	// Verify files
	fmt.Println()
	fmt.Println("=== Checking Files ===")
	missingFiles := 0
	okFiles := 0
	sizeMismatch := 0
	for _, file := range data.Files {
		normalizedPath := filepath.FromSlash(strings.ReplaceAll(file.Path, "\\", "/"))
		fullPath := filepath.Join(targetDir, normalizedPath)

		info, err := os.Stat(fullPath)
		if err != nil {
			fmt.Printf("  ✗ MISSING FILE: %s\n", fullPath)
			missingFiles++
			hasErrors = true
		} else if info.IsDir() {
			fmt.Printf("  ✗ NOT A FILE:   %s (exists as directory)\n", fullPath)
			missingFiles++
			hasErrors = true
		} else {
			// Check file size
			if file.Size >= 0 && info.Size() != file.Size {
				fmt.Printf("  ⚠ SIZE MISMATCH: %s (expected %d bytes, got %d bytes)\n", fullPath, file.Size, info.Size())
				sizeMismatch++
				hasErrors = true
			} else {
				okFiles++
			}
		}
	}
	fmt.Printf("  Files: %d OK, %d missing, %d wrong size\n", okFiles, missingFiles, sizeMismatch)

	// Check for extra files not in the listing
	// Only check within the root directories from the DMDE listing
	fmt.Println()
	fmt.Println("=== Checking for Extra Files ===")
	extraFiles := 0
	extraDirs := 0

	// Find root directories from the listing (top-level dirs that contain all others)
	rootDirs := findRootDirs(data.Directories, targetDir)
	if len(rootDirs) == 0 {
		fmt.Println("  No root directories found in listing, skipping extra files check")
	} else {
		fmt.Println("  Scanning within:")
		for _, rd := range rootDirs {
			fmt.Printf("    %s\n", rd)
		}
		for _, rootDir := range rootDirs {
			err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil // Skip files we can't access
				}
				if path == rootDir {
					return nil // Skip the root directory itself
				}

				lowerPath := strings.ToLower(path)
				if !expectedPaths[lowerPath] {
					if info.IsDir() {
						fmt.Printf("  ⚠ EXTRA DIR:  %s\n", path)
						extraDirs++
					} else {
						fmt.Printf("  ⚠ EXTRA FILE: %s\n", path)
						extraFiles++
					}
					hasWarnings = true
				}
				return nil
			})
			if err != nil {
				fmt.Printf("  Error walking directory: %v\n", err)
			}
		}
	}
	if extraFiles == 0 && extraDirs == 0 {
		fmt.Println("  No extra files or directories found")
	} else {
		fmt.Printf("  Found %d extra files, %d extra directories\n", extraFiles, extraDirs)
	}

	// Summary
	fmt.Println()
	fmt.Println("=== Summary ===")
	if missingDirs == 0 {
		fmt.Printf("  Directories: %d/%d ✓\n", existingDirs, len(data.Directories))
	} else {
		fmt.Printf("  Directories: %d/%d (%d missing)\n", existingDirs, len(data.Directories), missingDirs)
	}
	if missingFiles == 0 && sizeMismatch == 0 {
		fmt.Printf("  Files:       %d/%d ✓\n", okFiles, len(data.Files))
	} else {
		issues := []string{}
		if missingFiles > 0 {
			issues = append(issues, fmt.Sprintf("%d missing", missingFiles))
		}
		if sizeMismatch > 0 {
			issues = append(issues, fmt.Sprintf("%d wrong size", sizeMismatch))
		}
		fmt.Printf("  Files:       %d/%d (%s)\n", okFiles, len(data.Files), strings.Join(issues, ", "))
	}
	if extraFiles > 0 || extraDirs > 0 {
		fmt.Printf("  Extra items: %d files, %d directories\n", extraFiles, extraDirs)
	}

	// Note about parsed vs expected counts (only show if there's a discrepancy in parsing)
	fmt.Println()
	if data.ExpectedDirCount > 0 && len(data.Directories) != data.ExpectedDirCount {
		fmt.Printf("⚠ Warning: Parsed %d directories from listing, but file claims %d\n", len(data.Directories), data.ExpectedDirCount)
	}
	if data.ExpectedFileCount > 0 && len(data.Files) != data.ExpectedFileCount {
		fmt.Printf("⚠ Warning: Parsed %d files from listing, but file claims %d\n", len(data.Files), data.ExpectedFileCount)
	}

	if hasErrors {
		fmt.Println("✗ Verification completed with errors")
		os.Exit(1)
	} else if hasWarnings {
		fmt.Println("⚠ Verification completed with warnings")
	} else {
		fmt.Println("✓ Verification successful - all files and directories match")
	}
}

// stripCategoryFromPath removes the optional category prefix from a captured path.
// DMDE format has: flags flags [category] path
// We only strip prefixes that match known categories from the "File Categories:" line.
// This avoids false positives with folder names that might look like categories.
func stripCategoryFromPath(rawPath string, knownCategories map[string]bool) string {
	trimmed := strings.TrimLeft(rawPath, " \t")
	if len(knownCategories) == 0 {
		return trimmed
	}

	// Try to match a known category at the start followed by spaces
	for cat := range knownCategories {
		if strings.HasPrefix(trimmed, cat) {
			rest := trimmed[len(cat):]
			// Must be followed by at least one space
			if len(rest) > 0 && rest[0] == ' ' {
				return strings.TrimLeft(rest, " ")
			}
		}
	}

	return trimmed
}

// parseDMDEFile reads the DMDE file and extracts directories, files, and expected counts
func parseDMDEFile(filePath string) (*DMDEData, error) {
	// Read the entire file
	rawData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Convert from UTF-16 to UTF-8 if needed
	content := decodeFileContent(rawData)

	data := &DMDEData{}

	// First pass: extract known file categories from "File Categories:" line
	// Format: "File Categories:  f d " or "File Categories:  . , xf" (comma as separator)
	knownCategories := make(map[string]bool)
	categoryRegex := regexp.MustCompile(`File Categories:\s*(.*)$`)
	for _, line := range strings.Split(content, "\n") {
		if matches := categoryRegex.FindStringSubmatch(line); len(matches) > 1 {
			cats := strings.Fields(matches[1])
			for _, cat := range cats {
				// Skip commas - they're separators between categories, not actual categories
				if cat == "," {
					continue
				}
				knownCategories[cat] = true
			}
			break
		}
	}

	// Regex to match directory lines (contains <DIR>, then flags, then path ending with \)
	// Format: date time <DIR> flags flags [category]  path\
	// Example: 2025-01-17 13:29:18.927  <DIR>                 D---- ---A  f   Some_dir 1\
	// Note: category may be empty, so we only require 2 flag fields before the path
	dirRegex := regexp.MustCompile(`<DIR>\s+\S+\s+\S+\s+(.+\\)\s*$`)
	// Regex to match file lines (has size, no <DIR>, path without trailing \)
	// Format: date time  size  flags  flags  [category]  path
	// Note: category may be empty, so we only require 2 flag fields before the path
	fileRegex := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}\.\d+\s+(\d+)\s+\S+\s+\S+\s+(.+?)\s*$`)
	// Regex to extract the total directories count
	totalDirsRegex := regexp.MustCompile(`Total directories:\s*(\d+)`)
	// Regex to extract the total files count
	totalFilesRegex := regexp.MustCompile(`Total files:\s*(\d+)`)

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()

		// Check for directory entry
		if strings.Contains(line, "<DIR>") {
			matches := dirRegex.FindStringSubmatch(line)
			if len(matches) > 1 {
				// Strip optional category prefix using known categories
				dirPath := stripCategoryFromPath(matches[1], knownCategories)
				dirPath = strings.TrimSuffix(dirPath, "\\")
				data.Directories = append(data.Directories, dirPath)
			}
			continue
		}

		// Check for file entry (line starts with date, has size, no <DIR>)
		if len(line) > 0 && line[0] >= '0' && line[0] <= '9' {
			matches := fileRegex.FindStringSubmatch(line)
			if len(matches) > 2 {
				size, _ := strconv.ParseInt(matches[1], 10, 64)
				// Strip optional category prefix using known categories
				filePath := stripCategoryFromPath(matches[2], knownCategories)
				data.Files = append(data.Files, FileEntry{
					Path: filePath,
					Size: size,
				})
			}
			continue
		}

		// Check for total directories count
		if strings.Contains(line, "Total directories:") {
			matches := totalDirsRegex.FindStringSubmatch(line)
			if len(matches) > 1 {
				count, err := strconv.Atoi(matches[1])
				if err == nil {
					data.ExpectedDirCount = count
				}
			}
		}

		// Check for total files count
		if strings.Contains(line, "Total files:") {
			matches := totalFilesRegex.FindStringSubmatch(line)
			if len(matches) > 1 {
				count, err := strconv.Atoi(matches[1])
				if err == nil {
					data.ExpectedFileCount = count
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	return data, nil
}

// decodeFileContent detects encoding and converts to UTF-8 string
func decodeFileContent(data []byte) string {
	// Check for UTF-16 LE BOM (FF FE)
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE {
		return decodeUTF16LE(data[2:])
	}
	// Check for UTF-16 BE BOM (FE FF)
	if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
		return decodeUTF16BE(data[2:])
	}
	// Check for UTF-8 BOM (EF BB BF)
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return string(data[3:])
	}
	// Try to detect UTF-16 LE without BOM (common pattern: ASCII chars followed by 0x00)
	if len(data) >= 4 && data[1] == 0x00 && data[3] == 0x00 {
		return decodeUTF16LE(data)
	}
	// Try to detect UTF-16 BE without BOM
	if len(data) >= 4 && data[0] == 0x00 && data[2] == 0x00 {
		return decodeUTF16BE(data)
	}
	// Assume UTF-8
	return string(data)
}

// decodeUTF16LE converts UTF-16 Little Endian bytes to UTF-8 string
func decodeUTF16LE(data []byte) string {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	u16s := make([]uint16, len(data)/2)
	for i := 0; i < len(u16s); i++ {
		u16s[i] = uint16(data[2*i]) | uint16(data[2*i+1])<<8
	}
	return string(utf16.Decode(u16s))
}

// decodeUTF16BE converts UTF-16 Big Endian bytes to UTF-8 string
func decodeUTF16BE(data []byte) string {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	u16s := make([]uint16, len(data)/2)
	for i := 0; i < len(u16s); i++ {
		u16s[i] = uint16(data[2*i])<<8 | uint16(data[2*i+1])
	}
	return string(utf16.Decode(u16s))
}

// createDirectories creates all directories in the specified output location
// Returns (created count, skipped count, error)
func createDirectories(directories []string, outputDir string) (int, int, error) {
	createdCount := 0
	skippedCount := 0

	for _, dir := range directories {
		// Convert Windows-style path separators to OS-specific ones
		dir = filepath.FromSlash(strings.ReplaceAll(dir, "\\", "/"))

		fullPath := filepath.Join(outputDir, dir)

		// Check if directory already exists
		if info, err := os.Stat(fullPath); err == nil && info.IsDir() {
			skippedCount++
			fmt.Printf("  Exists:  %s\n", fullPath)
			continue
		}

		// Create the directory (and any parent directories)
		err := os.MkdirAll(fullPath, 0755)
		if err != nil {
			return createdCount, skippedCount, fmt.Errorf("failed to create directory '%s': %w", fullPath, err)
		}
		createdCount++
		fmt.Printf("  Created: %s\n", fullPath)
	}

	return createdCount, skippedCount, nil
}

// findRootDirs finds the top-level directories from the DMDE listing
// These are directories that are not subdirectories of any other directory in the listing
func findRootDirs(directories []string, targetDir string) []string {
	if len(directories) == 0 {
		return nil
	}

	// Normalize all directory paths
	normalized := make([]string, len(directories))
	for i, dir := range directories {
		normalized[i] = strings.ToLower(filepath.FromSlash(strings.ReplaceAll(dir, "\\", "/")))
	}

	// Find directories that are not subdirectories of any other
	var roots []string
	for _, dir := range normalized {
		isRoot := true
		for _, other := range normalized {
			if dir != other && strings.HasPrefix(dir, other+string(filepath.Separator)) {
				isRoot = false
				break
			}
		}
		if isRoot {
			// Only add if not already in roots (avoid duplicates)
			fullPath := filepath.Join(targetDir, dir)
			alreadyAdded := false
			for _, r := range roots {
				if strings.EqualFold(r, fullPath) {
					alreadyAdded = true
					break
				}
			}
			if !alreadyAdded {
				roots = append(roots, fullPath)
			}
		}
	}

	return roots
}
