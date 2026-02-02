# DirParser

DirParser is a small Go script that parses DMDE file recovery lists and can either recreate the directory structure or verify recovered files against the listing. It can come in handy for users of the free version od DMDE who can only recover files in the current panel.

## Build

```bash
go mod init dirparser
go build -o dirparser.exe .
```

## Usage

```
dirparser <mode> <dmde_file> <target_directory>
```

### Modes

- `-c`, `--create` — Create the directory structure from the DMDE listing (directories only).
- `-v`, `--verify` — Verify recovered files and directories against the DMDE listing.

### Examples

```bash
# Create directories only
dirparser -c "filelist.txt" "C:\Recovery\Output"

# Verify recovered files
dirparser -v "filelist.txt" "C:\Recovery\Output"
```

## Verify mode checks

- Missing directories
- Missing files
- File size mismatches
- Extra files/directories (scoped to the root directory from the listing)
