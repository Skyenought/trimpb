# TrimPB: Protobuf 裁剪工具


### 核心功能

*   **精简定义:** 移除所有未使用的服务、RPC、消息和枚举。
*   **双重用途:** 既可作为独立的**命令行工具**使用，也可作为 **Go 库 (SDK)** 集成到您的项目中。
*   **依赖感知:** 能够正确处理 `import` 语句，并保留跨文件的依赖关系。
*   **纯粹的库核心:** 核心裁剪逻辑是一个纯 Go 函数，不依赖于文件系统，使其极易测试和集成。

## 使用方式

### 1. 作为命令行工具

`trimpb` 的命令行界面简单直观。

#### 命令格式

```bash
trimpb [选项] <entry.proto...>
```

#### 参数

*   `<entry.proto...>`: **(必需)** 一个或多个作为裁剪起点的 `.proto` 文件。

#### 选项

*   `-m, --method <name>`: **(必需)** 指定要保留的 RPC 方法。可以多次使用此标志。
    *   如果 `<name>` 是 `Service.Method` 格式，将在所有指定的 `<entry.proto>` 文件内查找。
    *   如果 `<name>` 是 `package.Service.Method` 格式的完全限定名称，将在所有可发现的 `.proto` 文件中查找。
*   `-r, --recurse <目录>`: 指定 `.proto` 文件的根搜索目录（类似于 `protoc` 的 `-I` 参数）。如果你的 `.proto` 文件有 `import` 语句，这是必需的。可以多次使用。默认为 `.`。
*   `-o, --out <目录>`: 指定裁剪后文件的输出目录。默认为当前目录 (`.`)。
*   `--version`: 打印工具版本。
*   `-h, --help`: 显示帮助信息。

#### 示例

假设我们有如下目录结构：

```
example/
├── common.proto
└── project.proto
```

其中 `project.proto` 导入了 `common.proto`。我们只想保留 `project.proto` 中的 `CreateProject` 方法。

**命令:**

```bash
# 使用短名称，指定入口文件
trimpb \
  -m ProjectService.CreateProject \
  -r example \
  -o trimmed_output \
  example/project.proto
```

**执行过程:**

1.  工具接收 `example/project.proto` 作为入口文件。
2.  它会在 `example` 目录中查找所有 `.proto` 文件以解析依赖。
3.  它会在 `example/project.proto` 中查找名为 `ProjectService` 的服务，并找到其下的 `CreateProject` 方法。
4.  它会分析出该方法依赖 `CreateProjectRequest`, `CreateProjectResponse`, `Project` 以及 `common.proto` 中的 `Status` 和 `user.User`。
5.  最终，它会在 `trimmed_output` 目录下生成裁剪后的文件，并保持原有的目录结构。这些文件只包含 `CreateProject` 所需的最小定义。

### 2. 作为 Go 库 (SDK)

你可以直接在你的 Go 代码中导入并使用 `trimpb` 的核心逻辑。这对于自动化构建流程非常方便。

#### 安装依赖

```bash
# 将 github.com/your-repo/trimpb 替换为你的实际仓库地址
go get github.com/your-repo/trimpb@latest
```

#### `Trim` 和 `TrimMulti` 函数

本库对外暴露了两个核心函数：

*   `Trim(entryProtoFile string, ...)`: 一个方便的包装函数，用于处理单个入口文件的常见情况。
*   `TrimMulti(entryProtoFiles []string, ...)`: 更强大的核心函数，支持同时从多个入口文件开始裁剪，并合并它们的依赖关系。

#### 示例代码

下面是一个完整的 Go 程序，演示了如何调用 `trimpb` 库。

```go
package main

import (
	"fmt"
	"log"
	"trimpb" // 导入你的库
)

// setupProtoMap 模拟从文件系统加载 proto 文件到内存中
func setupProtoMap() map[string]string {
	return map[string]string{
		"common.proto": `
syntax = "proto3";
package common.v1;
message User { string name = 1; }
enum Status { ACTIVE = 0; INACTIVE = 1; }`,

		"project.proto": `
syntax = "proto3";
package project.v1;
import "common.proto";
service ProjectService {
  rpc CreateProject(CreateProjectRequest) returns (common.v1.User);
  rpc DeleteProject(DeleteProjectRequest) returns (DeleteProjectResponse);
}
message CreateProjectRequest { string project_name = 1; }
message DeleteProjectRequest { string project_id = 1; }
message DeleteProjectResponse { bool success = 1; }`,
	}
}

func main() {
	protoContents := setupProtoMap()

	// --- 示例 1: 使用 Trim() 处理单个入口文件 ---
	fmt.Println("--- 运行简单裁剪 (Trim) ---")
	entryFile := "project.proto"
	methodsToKeep := []string{"ProjectService.CreateProject"}

	trimmedResult, err := trimpb.Trim(entryFile, methodsToKeep, protoContents)
	if err != nil {
		log.Fatalf("Trim 失败: %v", err)
	}

	fmt.Println("裁剪后的 project.proto 内容:")
	fmt.Println(trimmedResult["project.proto"])
	// 返回结果中也会包含裁剪后的 common.proto


	// --- 示例 2: 使用 TrimMulti() 合并多个方法的依赖 ---
	fmt.Println("\n--- 运行多方法裁剪 (TrimMulti) ---")
	entryFiles := []string{"project.proto"}
	methodsToCombine := []string{
		"ProjectService.CreateProject", // 依赖 CreateProjectRequest 和 common.v1.User
		"ProjectService.DeleteProject", // 依赖 DeleteProjectRequest 和 DeleteProjectResponse
	}

	multiTrimmedResult, err := trimpb.TrimMulti(entryFiles, methodsToCombine, protoContents)
	if err != nil {
		log.Fatalf("TrimMulti 失败: %v", err)
	}
    
	fmt.Println("合并依赖后裁剪的 project.proto 内容:")
	fmt.Println(multiTrimmedResult["project.proto"])
}
```

## 项目结构

```
.
├── cmd/trimpb/         # 命令行工具的 main 包
│   └── main.go
├── example/            # 示例 .proto 文件
│   ├── common.proto
│   └── project.proto
├── go.mod              # Go 模块定义
├── trimpb.go           # 核心库逻辑
├── trimpb_test.go      # 核心库的单元测试
└── README.md           # 本文档
```

*   `trimpb.go`: 包含了所有与 Protobuf 解析、依赖分析和文件生成相关的**核心逻辑**。它对外暴露 `Trim` 和 `TrimMulti` 函数，并且**只对内存中的数据进行操作，不与文件系统直接交互**。
*   `cmd/trimpb/main.go`: 一个轻量级的包装器，**负责构建命令行工具**。它处理所有文件系统 I/O，包括：解析命令行参数、将 `.proto` 文件加载到内存、调用 `trimpb.go` 中的库函数，以及将裁剪结果写回磁盘。
*   `trimpb_test.go`: 对 `trimpb.go` 中核心逻辑的单元测试，确保其正确性和健壮性。

