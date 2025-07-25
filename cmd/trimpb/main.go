package main

import (
	"flag"
	"fmt"
	"github.com/Skyenought/trimpb"
	"os"
	"path/filepath"
	"strings"
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string         { return strings.Join(*s, ", ") }
func (s *stringSliceFlag) Set(value string) error { *s = append(*s, value); return nil }

func main() {
	var (
		showVersion  bool
		outputDir    string
		recursePaths stringSliceFlag
		methodNames  stringSliceFlag
	)

	flag.BoolVar(&showVersion, "version", false, "Print the version and exit.")
	flag.StringVar(&outputDir, "o", ".", "Specify the output directory for trimmed files.")
	flag.Var(&recursePaths, "r", "Specify a root dir for proto imports. Can be used multiple times.")
	flag.Var(&methodNames, "m", "Only keep the specified method and its dependents. Can be used multiple times.")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: trimpb [options] <entry.proto...>\n\n")
		fmt.Fprintf(os.Stderr, "A tool to trim protobuf files to only include specified RPC methods and their dependencies.\n\n")
		fmt.Fprintf(os.Stderr, "Arguments:\n")
		fmt.Fprintf(os.Stderr, "  <entry.proto...>    One or more proto files to start trimming from.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	// 校验参数
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "Error: At least one <entry.proto> file must be specified.")
		flag.Usage()
		os.Exit(1)
	}
	entryProtoFiles := flag.Args()

	if len(methodNames) == 0 {
		fmt.Fprintln(os.Stderr, "Error: At least one method must be specified with -m.")
		flag.Usage()
		os.Exit(1)
	}

	sourceRoots := []string(recursePaths)
	if len(sourceRoots) == 0 {
		sourceRoots = []string{"."}
		fmt.Printf("Info: No import path (-r) specified, defaulting to '.'\n")
	}

	// 1. Find and read all proto files into memory.
	protoContents, err := trimpb.LoadProtos(sourceRoots)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading proto files: %v\n", err)
		os.Exit(1)
	}
	if len(protoContents) == 0 {
		fmt.Fprintf(os.Stderr, "Error: No .proto files found in source roots: %v\n", sourceRoots)
		os.Exit(1)
	}
	fmt.Printf("Found and loaded %d proto files in total.\n", len(protoContents))

	// 2. Canonicalize all entry file paths to be relative to a source root.
	canonicalEntryFiles := make([]string, 0, len(entryProtoFiles))
	for _, entryFile := range entryProtoFiles {
		var canonicalPath string
		for _, root := range sourceRoots {
			// Check if the entry file is within this root.
			rel, err := filepath.Rel(root, entryFile)
			if err == nil && !strings.HasPrefix(rel, "..") {
				canonicalPath = rel
				break
			}
		}

		if canonicalPath == "" {
			fmt.Fprintf(os.Stderr, "Error: entry file '%s' is not located within any of the specified source roots: %v\n", entryFile, sourceRoots)
			os.Exit(1)
		}
		canonicalEntryFiles = append(canonicalEntryFiles, canonicalPath)
	}

	// 3. Call the multi-entry library function with the canonicalized paths and in-memory data.
	trimmedFiles, err := trimpb.TrimMulti(canonicalEntryFiles, methodNames, protoContents)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
		os.Exit(1)
	}

	// 4. Write the results from the returned map to the output directory.
	for path, content := range trimmedFiles {
		finalOutputPath := filepath.Join(outputDir, path)
		finalOutputDir := filepath.Dir(finalOutputPath)
		if err := os.MkdirAll(finalOutputDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating output directory %s: %v\n", finalOutputDir, err)
			os.Exit(1)
		}

		fmt.Printf("Writing trimmed file to: %s\n", finalOutputPath)
		if err := os.WriteFile(finalOutputPath, []byte(content), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing to file %s: %v\n", finalOutputPath, err)
			os.Exit(1)
		}
	}
}
