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
		outputDir   string
		importPaths stringSliceFlag
		methodNames stringSliceFlag
	)

	flag.StringVar(&outputDir, "o", ".", "指定裁剪后文件的输出目录。")
	flag.Var(&importPaths, "I", "指定一个 .proto 文件的搜索路径（类似 protoc 的 -I）。可多次使用。")
	flag.Var(&methodNames, "m", "仅保留指定的方法及其依赖。可多次使用。")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "用法: trimpb [选项] <entry.proto...>\n\n")
		fmt.Fprintf(os.Stderr, "一个用于裁剪 Protobuf 文件的工具，使其仅包含指定的 RPC 方法及其依赖项。\n\n")
		fmt.Fprintf(os.Stderr, "参数:\n")
		fmt.Fprintf(os.Stderr, "  <entry.proto...>    一个或多个作为裁剪起点的 .proto 文件。\n\n")
		fmt.Fprintf(os.Stderr, "选项:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "错误: 必须至少指定一个 <entry.proto> 文件。")
		flag.Usage()
		os.Exit(1)
	}
	entryProtoFiles := flag.Args()

	if len(methodNames) == 0 {
		fmt.Fprintln(os.Stderr, "错误: 必须使用 -m 标志至少指定一个方法。")
		flag.Usage()
		os.Exit(1)
	}

	sourceRoots := []string(importPaths)
	if len(sourceRoots) == 0 {
		sourceRoots = []string{"."}
		fmt.Printf("提示: 未指定导入路径 (-I)，默认使用当前目录 '.'\n")
	}

	// 1. 将所有入口文件路径规范化，使其成为相对于某个导入路径的相对路径。
	//    这是因为 `parser.ParseFiles` 需要接收这种相对路径。
	canonicalEntryFiles := make([]string, 0, len(entryProtoFiles))
	for _, entryFile := range entryProtoFiles {
		var canonicalPath string
		// 优先匹配绝对路径
		absEntryFile, err := filepath.Abs(entryFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误: 无法获取入口文件 '%s' 的绝对路径: %v\n", entryFile, err)
			os.Exit(1)
		}

		for _, root := range sourceRoots {
			absRoot, err := filepath.Abs(root)
			if err != nil {
				fmt.Fprintf(os.Stderr, "错误: 无法获取导入路径 '%s' 的绝对路径: %v\n", root, err)
				os.Exit(1)
			}
			rel, err := filepath.Rel(absRoot, absEntryFile)
			if err == nil && !strings.HasPrefix(rel, "..") {
				canonicalPath = filepath.ToSlash(rel)
				break
			}
		}

		if canonicalPath == "" {
			fmt.Fprintf(os.Stderr, "错误: 入口文件 '%s' 不在任何指定的导入路径 (-I) 内: %v\n", entryFile, sourceRoots)
			os.Exit(1)
		}
		canonicalEntryFiles = append(canonicalEntryFiles, canonicalPath)
	}
	fmt.Printf("规范化后的入口文件: %v\n", canonicalEntryFiles)

	// 2. 直接调用新的库函数，让它处理文件系统操作。
	trimmedFiles, err := trimpb.TrimWithImportPaths(canonicalEntryFiles, methodNames, sourceRoots)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n错误: %v\n", err)
		os.Exit(1)
	}

	// 3. 将结果从 map 写入到输出目录。
	for path, content := range trimmedFiles {
		finalOutputPath := filepath.Join(outputDir, path)
		finalOutputDir := filepath.Dir(finalOutputPath)
		if err := os.MkdirAll(finalOutputDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "错误：创建输出目录 %s 失败: %v\n", finalOutputDir, err)
			os.Exit(1)
		}

		fmt.Printf("正在将裁剪后的文件写入: %s\n", finalOutputPath)
		if err := os.WriteFile(finalOutputPath, []byte(content), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "错误：写入文件 %s 失败: %v\n", finalOutputPath, err)
			os.Exit(1)
		}
	}
}
