# TrimPB: Protobuf 裁剪与清理工具

### 核心功能

*   **精简定义:** 根据指定的 RPC 方法，移除所有未使用的服务、RPC、消息和枚举。
*   **智能模式切换:** 当指定方法时，进行精确裁剪；当不指定任何方法时，自动切换到 **“清理模式”**，保留所有服务和方法，仅移除未被引用的类型定义和 `import`。
*   **双重用途:** 既可作为独立的**命令行工具**使用，也可作为 **Go 库 (SDK)** 集成到您的项目中。
*   **依赖感知:** 能够正确处理 `import` 语句，并保留跨文件的依赖关系。
*   **纯粹的库核心:** 核心裁剪逻辑是一个纯 Go 函数，不依赖于文件系统，使其极易测试和集成。

## 使用方式

### 作为 Go 库 (SDK)

你可以直接在你的 Go 代码中导入并使用 `trimpb` 的核心逻辑。

---

#### 方式 A: 内存操作 (推荐用于测试和解耦)

使用 `TrimMulti` 函数，它对一个预先加载到内存的 `map`进行操作，与文件系统完全解耦。

*   **函数:** `TrimMulti(entryProtoFiles []string, methodNames []string, importPaths []string, protoContents map[string]string)`
*   **关键行为**: 当 `methodNames` 切片不为空时，执行精确裁剪。当 `methodNames` **切片为空**时，执行“清理模式”。
*   **优点:** 无文件 I/O，便于进行快速、可靠的单元测试。

**示例代码:**

```go
package main

import (
	"fmt"
	"log"
	"github.com/Skyenought/trimpb"
)

// 模拟从文件系统加载 proto 文件到内存中
func setupProtoMap() map[string]string {
	return map[string]string{
		"common.proto": `
syntax = "proto3";
package common.v1;
message Status { int32 code = 1; }`,

		"project.proto": `
syntax = "proto3";
package project.v1;
import "common.proto";
service ProjectService {
  rpc CreateProject(CreateProjectRequest) returns (common.v1.Status);
  rpc DeleteProject(DeleteProjectRequest) returns (common.v1.Status);
}
message CreateProjectRequest { string name = 1; }
message DeleteProjectRequest { string id = 1; }
message UnusedMessage { bool flag = 1; }`,
	}
}

func main() {
    protoContents := setupProtoMap()
    entryFile := "project.proto"
	importPaths := []string{"."} // 假设所有文件都在根目录

    // 示例 1: 精确裁剪模式
    methodsToKeep := []string{"ProjectService.CreateProject"}
    trimmedResult, err := trimpb.TrimMulti([]string{entryFile}, methodsToKeep, importPaths, protoContents)
    if err != nil {
        log.Fatalf("TrimMulti (裁剪模式) 失败: %v", err)
    }
    fmt.Println("--- 裁剪模式输出 (只保留 CreateProject) ---")
    fmt.Println(trimmedResult["project.proto"])


    // 示例 2: 清理模式 (传入空切片)
    cleanupResult, err := trimpb.TrimMulti([]string{entryFile}, []string{}, importPaths, protoContents)
    if err != nil {
        log.Fatalf("TrimMulti (清理模式) 失败: %v", err)
    }
    fmt.Println("\n--- 清理模式输出 (保留所有方法，移除 UnusedMessage) ---")
    fmt.Println(cleanupResult["project.proto"])
}
```

---

#### 方式 B: 文件系统操作 (推荐用于构建脚本和工具)

使用 `TrimWithImportPaths` (或类似功能的函数)，它直接接收导入路径，行为与 `protoc` 命令行工具类似。

*   **函数:** `TrimWithImportPaths(entryProtoFiles []string, methodNames []string, importPaths []string)`
*   **关键行为**: 同样地，当 `methodNames` **切片为空**时，执行“清理模式”。
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

    // 使用 FQN 更稳妥
    methodsToKeep := []string{"project.v1.ProjectService.CreateProject"}

    // 示例 1: 精确裁剪模式
    trimmedResult, err := trimpb.TrimWithImportPaths([]string{entryFile}, methodsToKeep, importPaths)
    if err != nil {
        log.Fatalf("TrimWithImportPaths (裁剪模式) 失败: %v", err)
    }
    fmt.Println("--- 裁剪模式输出 ---")
    fmt.Println(trimmedResult["project.proto"])

    // 示例 2: 清理模式 (传入空切片)
    // 这将保留 'example/project.proto' 中的所有服务和方法，但移除其中未被引用的类型
    cleanupResult, err := trimpb.TrimWithImportPaths([]string{entryFile}, []string{}, importPaths)
    if err != nil {
        log.Fatalf("TrimWithImportPaths (清理模式) 失败: %v", err)
    }
    fmt.Println("\n--- 清理模式输出 ---")
    fmt.Println(cleanupResult["project.proto"])
}
```

---

## 项目结构

```
.
├── go.mod              # Go 模块定义
├── trimpb.go           # 核心库逻辑
├── trimpb_test.go      # 核心库的单元测试
└── README.md           # 本文档
```

*   `trimpb.go`: 包含了所有与 Protobuf 解析、依赖分析和文件生成相关的**核心逻辑**。
*   `trimpb_test.go`: 对 `trimpb.go` 中核心逻辑的单元测试，确保其正确性和健壮性。
