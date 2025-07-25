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
*   `-I <目录>`: 指定 `.proto` 文件的根搜索目录（**完全等同于 `protoc` 的 `-I` 或 `--proto_path` 参数**）。如果你的 `.proto` 文件有 `import` 语句，这是必需的。可以多次使用。默认为 `.`。
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

其中 `project.proto` 导入了 `common.proto` (`import "common.proto";`)。我们只想保留 `project.proto` 中的 `CreateProject` 方法。

**命令:**

```bash
# 使用 -I 指定 import path，这对于解析 project.proto 中的 import 语句至关重要
trimpb \
  -m ProjectService.CreateProject \
  -I example \
  -o trimmed_output \
  example/project.proto
```

**执行过程:**

1.  工具接收 `example/project.proto` 作为入口文件。
2.  它会将 `example` 目录添加为导入搜索路径（因为我们使用了 `-I example`）。
3.  当解析 `example/project.proto` 时，它会遇到 `import "common.proto";`。它会在 `example` 目录中查找并成功找到 `example/common.proto`。
4.  它会在 `example/project.proto` 中查找名为 `ProjectService` 的服务，并找到其下的 `CreateProject` 方法。
5.  它会分析出该方法依赖 `CreateProjectRequest`, `CreateProjectResponse`, `Project` 以及 `common.proto` 中的 `Status` 和 `user.User`。
6.  最终，它会在 `trimmed_output` 目录下生成裁剪后的文件，并保持原有的目录结构。这些文件只包含 `CreateProject` 所需的最小定义。

### 2. 作为 Go 库 (SDK)

你可以直接在你的 Go 代码中导入并使用 `trimpb` 的核心逻辑。本库提供了两种使用方式，以适应不同需求。

---

#### 方式 A: 内存操作 (推荐用于测试和解耦)

使用 `TrimMulti` 函数，它对一个预先加载到内存的 `map`进行操作，与文件系统完全解耦。

*   **函数:** `TrimMulti(entryProtoFiles []string, methodNames []string, protoContents map[string]string)`
*   **优点:** 无文件 I/O，便于进行快速、可靠的单元测试。

**示例代码:**

```go
package main

import (
	"fmt"
	"log"
	"github.com/Skyenought/trimpb"
)

// setupProtoMap 模拟从文件系统加载 proto 文件到内存中
// 注意：map 的 key 必须是相对 import root 的路径。
func setupProtoMap() map[string]string {
	return map[string]string{
		"common.proto": `
syntax = "proto3";
package common.v1;
message User { string name = 1; }`,
		"project.proto": `
syntax = "proto3";
package project.v1;
import "common.proto";
service ProjectService {
  rpc CreateProject(CreateProjectRequest) returns (common.v1.User);
}
message CreateProjectRequest { string project_name = 1; }`,
	}
}

func main() {
    protoContents := setupProtoMap()
    entryFile := "project.proto"
    methodsToKeep := []string{"ProjectService.CreateProject"}

    // 使用 TrimMulti 进行内存操作
    trimmedResult, err := trimpb.TrimMulti([]string{entryFile}, methodsToKeep, protoContents)
    if err != nil {
        log.Fatalf("TrimMulti 失败: %v", err)
    }

    fmt.Println(trimmedResult["project.proto"])
}
```

---

#### 方式 B: 文件系统操作 (推荐用于构建脚本和工具)

使用新增的 `TrimWithImportPaths` 函数，它直接接收导入路径，行为与 `protoc` 命令行工具类似。

*   **函数:** `TrimWithImportPaths(entryProtoFiles []string, methodNames []string, importPaths []string)`
*   **优点:** 调用简单直接，无需手动读取文件。

**示例代码:**

```go
package main

import (
	"fmt"
	"log"
	"github.com/Skyenought/trimpb"
)

func main() {
    // 假设你的 .proto 文件在 'example' 目录下
    importPaths := []string{"example"}
    entryFile := "project.proto" // 必须是相对于 importPaths 的路径
    methodsToKeep := []string{"project.v1.ProjectService.CreateProject"} // 使用 FQN 更稳妥

    // 使用 TrimWithImportPaths 直接操作文件系统
    trimmedResult, err := trimpb.TrimWithImportPaths([]string{entryFile}, methodsToKeep, importPaths)
    if err != nil {
        log.Fatalf("TrimWithImportPaths 失败: %v", err)
    }

    fmt.Println(trimmedResult["project.proto"])
}
```

---

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

*   `trimpb.go`: 包含了所有与 Protobuf 解析、依赖分析和文件生成相关的**核心逻辑**。
*   `cmd/trimpb/main.go`: 一个轻量级的包装器，**负责构建命令行工具**。它处理所有文件系统 I/O，并调用 `trimpb.go` 中的库函数。
*   `trimpb_test.go`: 对 `trimpb.go` 中核心逻辑的单元测试，确保其正确性和健壮性。
