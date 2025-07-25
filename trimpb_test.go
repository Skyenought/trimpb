package trimpb

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadProtoFiles 读取一组相对于根目录的 protobuf 文件，
// 并返回一个从它们的完整路径到其内容的映射。
// 这模拟了从项目根加载文件的过程。
func loadProtoFiles(t *testing.T, rootDir string, relativeFiles ...string) map[string]string {
	t.Helper()
	contents := make(map[string]string)
	for _, relPath := range relativeFiles {
		// map 的 key 是从项目角度出发的完整路径
		fullPath := filepath.Join(rootDir, relPath)
		bytes, err := os.ReadFile(fullPath)
		require.NoError(t, err, "未能读取测试 proto 文件: %s", fullPath)
		contents[fullPath] = string(bytes)
	}
	return contents
}

func TestTrimMulti(t *testing.T) {
	// 设置: 为复杂的 'muit' 示例定义所有 proto 文件
	muitProtoFiles := []string{
		"api/v1/commerce_service.proto",
		"api/v1/common_messages.proto",
		"common/types/base.proto",
		"common/types/money.proto",
		"services/order/item.proto",
		"services/order/order.proto",
		"services/product/product.proto",
		"services/product/review.proto",
		"services/user/profile.proto",
		"services/user/user.proto",
	}

	testCases := []struct {
		name                string
		entryProtoFiles     []string
		methodNames         []string
		importPaths         []string
		protoContents       map[string]string
		expectedOutputKeys  []string
		expectedContains    map[string][]string // map[filePath][]substrings
		expectedNotContains map[string][]string // map[filePath][]substrings
		expectError         bool
		errorContains       string
	}{
		{
			name:            "清理模式 - methodNames 为空时保留所有方法并移除未使用项",
			entryProtoFiles: []string{"project.proto"},
			methodNames:     []string{}, // <-- 关键：空切片触发清理模式
			importPaths:     []string{"example"},
			protoContents: loadProtoFiles(t, "example",
				"project.proto",
				"common.proto",
				"domain/user.proto",
			),
			expectedOutputKeys: []string{
				"example/project.proto",
				"example/common.proto",
				"example/domain/user.proto",
			},
			expectedContains: map[string][]string{
				"example/project.proto": {
					`service ProjectService`,
					`rpc CreateProject`,
					`rpc DeleteProject`,     // 应该被保留
					`rpc GetProjectDetails`, // 应该被保留
					`message Project`,
					`message CreateProjectRequest`,
					`message DeleteProjectRequest`, // 应该被保留
				},
				"example/domain/user.proto": {
					`message User`,
					`message PersonalInfo`, // GetProjectDetails 用到了它，应该被保留
				},
			},
			expectedNotContains: map[string][]string{
				"example/project.proto": {
					`message UnrelatedMessage`, // 这个 message 没有任何 RPC 使用，应该被移除
				},
			},
			expectError: false,
		},
		{
			name:            "简单项目裁剪 - CreateProject",
			entryProtoFiles: []string{"project.proto"},
			methodNames:     []string{"ProjectService.CreateProject"},
			importPaths:     []string{"example"},
			protoContents: loadProtoFiles(t, "example",
				"project.proto",
				"common.proto",
				"domain/user.proto",
			),
			expectedOutputKeys: []string{
				"example/project.proto",
				"example/common.proto",
				"example/domain/user.proto",
			},
			expectedContains: map[string][]string{
				"example/project.proto": {
					`service ProjectService`,
					`rpc CreateProject`,
					`message Project`,
					`message CreateProjectRequest`,
					`message CreateProjectResponse`,
					`import "common.proto";`,
					`import "domain/user.proto";`,
				},
				"example/common.proto": {
					`enum Status`,
					`ACTIVE = 1;`,
				},
				"example/domain/user.proto": {
					`message User`,
					`string user_id = 1;`,
				},
			},
			expectedNotContains: map[string][]string{
				"example/project.proto": {
					`rpc DeleteProject`,
					`message UnrelatedMessage`,
					`GetProjectDetails`, // 另一个 rpc
				},
				"example/domain/user.proto": {
					`message PersonalInfo`, // 这个应该被裁剪掉
				},
			},
		},
		{
			name:            "同级目录导入裁剪 - Thrift 示例",
			entryProtoFiles: []string{"turing/question_search/qs_service.proto"},
			methodNames:     []string{"turing.qs.QuestionSearchService.GetQuestionSearchFeedback"},
			importPaths:     []string{"example/thrift/alice_edu/service"},
			protoContents: loadProtoFiles(t, "example/thrift/alice_edu/service",
				"turing/question_search/qs_service.proto",
				"common/feedback.proto",
			),
			expectedOutputKeys: []string{
				"example/thrift/alice_edu/service/turing/question_search/qs_service.proto",
				"example/thrift/alice_edu/service/common/feedback.proto",
			},
			expectedContains: map[string][]string{
				"example/thrift/alice_edu/service/turing/question_search/qs_service.proto": {
					`service QuestionSearchService`,
					`rpc GetQuestionSearchFeedback`,
					`import "common/feedback.proto";`,
				},
				"example/thrift/alice_edu/service/common/feedback.proto": {
					`message Feedback`,
				},
			},
			expectedNotContains: map[string][]string{
				"example/thrift/alice_edu/service/common/feedback.proto": {
					`message UnusedMessage`,
				},
			},
		},
		{
			name:            "复杂 Muit 示例 - GetUser",
			entryProtoFiles: []string{"api/v1/commerce_service.proto"},
			methodNames:     []string{"api.v1.CommerceService.GetUser"},
			importPaths:     []string{"example/muit"},
			protoContents:   loadProtoFiles(t, "example/muit", muitProtoFiles...),
			expectedOutputKeys: []string{
				"example/muit/api/v1/commerce_service.proto",
				"example/muit/api/v1/common_messages.proto",
				"example/muit/common/types/base.proto",
				"example/muit/services/user/user.proto",
				"example/muit/services/user/profile.proto",
			},
			expectedContains: map[string][]string{
				"example/muit/api/v1/commerce_service.proto": {
					`service CommerceService`,
					`rpc GetUser`,
					`import "api/v1/common_messages.proto";`,
					`import "services/user/user.proto";`,
				},
				"example/muit/api/v1/common_messages.proto": {
					`message GetRequest`,
					`import "common/types/base.proto";`,
				},
				"example/muit/common/types/base.proto": {
					`message UUID`,
					`enum Status`,
				},
				"example/muit/services/user/user.proto": {
					`message User`,
					`import "services/user/profile.proto";`,
				},
				"example/muit/services/user/profile.proto": {
					`message UserProfile`,
				},
			},
			expectedNotContains: map[string][]string{
				"example/muit/api/v1/commerce_service.proto": {
					`service TestService`, // 应该被移除
					`rpc CreateUser`,
					`import "services/product/product.proto";`, // import 应该被移除
				},
				"example/muit/api/v1/common_messages.proto": {
					`message ListRequest`, // 应该被移除
				},
			},
		},
		{
			name:            "复杂Muit示例-PlaceOrder",
			entryProtoFiles: []string{"api/v1/commerce_service.proto"},
			methodNames:     []string{"api.v1.CommerceService.PlaceOrder"},
			importPaths:     []string{"example/muit"},
			protoContents:   loadProtoFiles(t, "example/muit", muitProtoFiles...),
			expectedOutputKeys: []string{
				"example/muit/api/v1/commerce_service.proto",
				"example/muit/services/order/order.proto",
				"example/muit/common/types/base.proto",
				"example/muit/common/types/money.proto",
				"example/muit/services/order/item.proto",
				"example/muit/services/product/product.proto", // 通过 item.proto 导入
			},
			expectedContains: map[string][]string{
				"example/muit/api/v1/commerce_service.proto": {
					`service CommerceService`,
					`rpc PlaceOrder`,
					`import "services/order/order.proto";`,
				},
				"example/muit/services/order/order.proto": {
					`message Order`,
					`import "services/order/item.proto";`,
				},
				"example/muit/services/order/item.proto": {
					`message OrderItem`,
					`import "services/product/product.proto";`,
				},
				"example/muit/services/product/product.proto": {
					`message Product`,
					`import "common/types/money.proto";`,
				},
			},
			expectedNotContains: map[string][]string{
				"example/muit/api/v1/commerce_service.proto": {
					`rpc GetUser`,
					`import "api/v1/common_messages.proto";`, // PlaceOrder 不需要这个
				},
			},
		},
		{
			name:            "错误 - 找不到方法",
			entryProtoFiles: []string{"project.proto"},
			methodNames:     []string{"ProjectService.NonExistentMethod"},
			importPaths:     []string{"example"},
			protoContents: loadProtoFiles(t, "example",
				"project.proto",
				"common.proto",
				"domain/user.proto", // <--- 修复：添加这个缺失的文件
			),
			expectError:   true,
			errorContains: "method 'ProjectService.NonExistentMethod' not found",
		},
		{
			name:            "错误 - 解析错误, 缺少导入",
			entryProtoFiles: []string{"project.proto"},
			methodNames:     []string{"ProjectService.CreateProject"},
			importPaths:     []string{"example"},
			protoContents: loadProtoFiles(t, "example",
				"project.proto",
			),
			expectError: true,
			// --- 修复：更新这里的期望错误字符串 ---
			errorContains: `example/common.proto: file does not exist`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 在测试期间抑制函数中的标准输出
			rescueStdout := os.Stdout
			_, w, _ := os.Pipe()
			os.Stdout = w

			trimmedResult, err := TrimMulti(tc.entryProtoFiles, tc.methodNames, tc.importPaths, tc.protoContents)

			w.Close()
			os.Stdout = rescueStdout

			if tc.expectError {
				require.Error(t, err)
				if tc.errorContains != "" {
					assert.Contains(t, err.Error(), tc.errorContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, trimmedResult)

			// 检查输出中的文件是否与预期完全一致
			assert.Len(t, trimmedResult, len(tc.expectedOutputKeys), "输出文件的数量与预期不符")
			for _, key := range tc.expectedOutputKeys {
				_, ok := trimmedResult[key]
				assert.True(t, ok, "预期的输出文件 key 未找到: %s", key)
			}

			// 检查文件内容
			for file, substrings := range tc.expectedContains {
				content, ok := trimmedResult[file]
				require.True(t, ok, "预期文件在结果中缺失: %s", file)
				for _, sub := range substrings {
					assert.Contains(t, content, sub, fmt.Sprintf("文件 %s 应该包含 '%s'", file, sub))
				}
			}

			for file, substrings := range tc.expectedNotContains {
				content, ok := trimmedResult[file]
				require.True(t, ok, "预期文件在结果中缺失: %s", file)
				for _, sub := range substrings {
					assert.NotContains(t, content, sub, fmt.Sprintf("文件 %s 不应该包含 '%s'", file, sub))
				}
			}
		})
	}
}
