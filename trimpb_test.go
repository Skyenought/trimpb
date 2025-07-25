package trimpb

import (
	"bytes"
	"fmt"
	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/desc/protoparse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"log"
	"os"
	"testing"
)

func Test_UsageExample(t *testing.T) {
	// --- Setup: Define the content of all our proto files in memory ---
	// This simulates loading the files, making the example self-contained.
	// The keys of the map must match the paths used in the `import` statements.

	protoContents := map[string]string{
		"example/common.proto": `
syntax = "proto3";
package project.v1;
option go_package = "example/projectv1";

enum Status {
  STATUS_UNSPECIFIED = 0;
  ACTIVE = 1;
  INACTIVE = 2;
}`,

		"example/domain/user.proto": `
syntax = "proto3";
package project.v1.user;
option go_package = "example/projectv1/user";

message User {
  string user_id = 1;
  string display_name = 2;
}

message PersonalInfo {
  string email = 1;
  string phone_number = 2;
}`,

		"example/project.proto": `
syntax = "proto3";
package project.v1;
import "example/common.proto";
import "example/domain/user.proto";
option go_package = "example/projectv1";

message Project {
  string project_id = 1;
  string name = 2;
  Status status = 3;
  user.User owner = 4;
}

service ProjectService {
  // Keeps Project, Status, User, CreateProjectRequest, CreateProjectResponse
  rpc CreateProject(CreateProjectRequest) returns (CreateProjectResponse);

  // Keeps only DeleteProjectRequest, DeleteProjectResponse
  rpc DeleteProject(DeleteProjectRequest) returns (DeleteProjectResponse);
  
  // Keeps PersonalInfo, DeleteProjectRequest
  rpc GetProjectDetails(user.PersonalInfo) returns (DeleteProjectRequest);
}

message CreateProjectRequest {
  string name = 1;
  string owner_user_id = 2;
}
message CreateProjectResponse {
  Project project = 1;
}
message DeleteProjectRequest {
  string project_id = 1;
}
message DeleteProjectResponse {
  bool success = 1;
}`,
	}

	// =================================================================
	//  Example 1: Using trimpb.Trim (Single Entry Point)
	// =================================================================
	// We want to keep only the `CreateProject` method and its dependencies.

	fmt.Println("--- Running Example 1: Using trimpb.Trim (Single Entry Point) ---")

	// 1. Define the parameters for the simple Trim function.
	entryFile := "example/project.proto"
	methodsToKeepForTrim := []string{"ProjectService.CreateProject"}

	// 2. Call the Trim function.
	trimmedFiles, err := Trim(entryFile, methodsToKeepForTrim, protoContents)
	if err != nil {
		log.Fatalf("Trim failed: %v", err)
	}

	// 3. Print the results.
	fmt.Println("\n[SUCCESS] Trim operation complete. Resulting files:")
	for path, content := range trimmedFiles {
		fmt.Printf("\n----- Trimmed content of: %s -----\n", path)
		fmt.Println(content)
	}
	fmt.Println("Notice how only Project, Status, User, and the Create* messages are kept.")
	fmt.Println("=================================================================")

	// =================================================================
	//  Example 2: Using trimpb.TrimMulti (Multiple Entry Points & Methods)
	// =================================================================
	// We want to keep two unrelated methods: `DeleteProject` and `GetProjectDetails`.
	// This will test if the dependency graphs are correctly merged.

	fmt.Println("--- Running Example 2: Using trimpb.TrimMulti ---")

	// 1. Define the parameters for the TrimMulti function.
	// Even if methods are in the same file, you can list it as an entry point.
	entryFilesMulti := []string{"example/project.proto"}
	methodsToKeepForTrimMulti := []string{
		"ProjectService.DeleteProject",     // Depends on DeleteProjectRequest/Response
		"ProjectService.GetProjectDetails", // Depends on PersonalInfo and DeleteProjectRequest
	}

	// 2. Call the TrimMulti function.
	trimmedFilesMulti, err := TrimMulti(entryFilesMulti, methodsToKeepForTrimMulti, protoContents)
	if err != nil {
		log.Fatalf("TrimMulti failed: %v", err)
	}

	// 3. Print the results.
	fmt.Println("\n[SUCCESS] TrimMulti operation complete. Resulting files:")
	for path, content := range trimmedFilesMulti {
		fmt.Printf("\n----- Trimmed content of: %s -----\n", path)
		fmt.Println(content)
	}
	fmt.Println("Notice how the dependencies are merged:")
	fmt.Println("- `DeleteProjectRequest` is kept because it's used by both methods.")
	fmt.Println("- `PersonalInfo` is kept for GetProjectDetails.")
	fmt.Println("- `DeleteProjectResponse` is kept for DeleteProject.")
	fmt.Println("- `Project`, `Status`, and `User` from the first example are now gone.")
	fmt.Println("=================================================================")

}

func TestTrimFunctions(t *testing.T) {
	// --- Test Fixtures: Proto file contents ---
	commonProtoContent := `
syntax = "proto3";
package common.v1;
option go_package = "example/commonv1";

message User {
  string user_id = 1;
  string display_name = 2;
}
enum Status {
  STATUS_UNSPECIFIED = 0;
  ACTIVE = 1;
  INACTIVE = 2;
}
message UnusedCommonMessage {
	string id = 1;
}`

	projectProtoContent := `
syntax = "proto3";
package project.v1;
import "common.proto";
option go_package = "example/projectv1";

message Project {
  string project_id = 1;
  string name = 2;
  common.v1.Status status = 3;
  common.v1.User owner = 4;
}
message UnrelatedMessage {
  string data = 1;
}
service ProjectService {
  rpc CreateProject(CreateProjectRequest) returns (CreateProjectResponse);
  rpc DeleteProject(DeleteProjectRequest) returns (DeleteProjectResponse);
}
message CreateProjectRequest {
  string name = 1;
  string owner_user_id = 2;
}
message CreateProjectResponse {
  Project project = 1;
}
message DeleteProjectRequest {
  string project_id = 1;
}
message DeleteProjectResponse {
  bool success = 1;
}`

	// New proto file for multi-entry test
	billingProtoContent := `
syntax = "proto3";
package billing.v1;
import "common.proto";

message Invoice {
	string invoice_id = 1;
	common.v1.User billing_contact = 2;
}
message GenerateInvoiceRequest {
	string user_id_for_invoice = 1;
}
message GenerateInvoiceResponse {
	Invoice invoice = 1;
}
service BillingService {
	rpc GenerateInvoice(GenerateInvoiceRequest) returns (GenerateInvoiceResponse);
}`

	protoContents := map[string]string{
		"common.proto":  commonProtoContent,
		"project.proto": projectProtoContent,
		"billing.proto": billingProtoContent,
	}

	t.Run("Trim_SingleEntryPoint_Wrapper", func(t *testing.T) {
		entryFile := "project.proto"
		methods := []string{"ProjectService.CreateProject"}

		var trimmedFiles map[string]string
		var err error
		_ = captureOutput(t, func() {
			// Test the original Trim function
			trimmedFiles, err = Trim(entryFile, methods, protoContents)
		})

		require.NoError(t, err)
		require.Len(t, trimmedFiles, 2, "Should have trimmed project and common")
		require.Contains(t, trimmedFiles, "project.proto")
		require.Contains(t, trimmedFiles, "common.proto")
		assert.NotContains(t, trimmedFiles, "billing.proto")
	})

	t.Run("TrimMulti_MultiEntryPoint", func(t *testing.T) {

		entryFiles := []string{"project.proto", "billing.proto"}
		methods := []string{"ProjectService.DeleteProject", "BillingService.GenerateInvoice"}

		var trimmedFiles map[string]string
		var err error
		_ = captureOutput(t, func() {
			// Test the new TrimMulti function
			trimmedFiles, err = TrimMulti(entryFiles, methods, protoContents)
		})

		require.NoError(t, err)
		require.Len(t, trimmedFiles, 3, "Should have trimmed project, billing, and common")
		require.Contains(t, trimmedFiles, "project.proto")
		require.Contains(t, trimmedFiles, "billing.proto")
		require.Contains(t, trimmedFiles, "common.proto")

		parser := protoparse.Parser{Accessor: protoparse.FileContentsFromMap(trimmedFiles)}
		fds, err := parser.ParseFiles("project.proto", "billing.proto", "common.proto")
		require.NoError(t, err)

		fdMap := make(map[string]*desc.FileDescriptor)
		for _, fd := range fds {
			fdMap[fd.GetName()] = fd
		}

		// Check project.proto
		projectFd := fdMap["project.proto"]
		require.NotNil(t, projectFd)
		projectSvc := projectFd.FindService("project.v1.ProjectService")
		require.NotNil(t, projectSvc)
		assert.Nil(t, projectSvc.FindMethodByName("CreateProject"), "CreateProject should be trimmed")
		assert.NotNil(t, projectSvc.FindMethodByName("DeleteProject"), "DeleteProject should be kept")
		assert.NotNil(t, projectFd.FindMessage("project.v1.DeleteProjectRequest"))
		assert.Nil(t, projectFd.FindMessage("project.v1.Project"), "Project message is not a dependency of DeleteProject and should be trimmed")

		// Check billing.proto
		billingFd := fdMap["billing.proto"]
		require.NotNil(t, billingFd)
		billingSvc := billingFd.FindService("billing.v1.BillingService")
		require.NotNil(t, billingSvc)
		assert.NotNil(t, billingSvc.FindMethodByName("GenerateInvoice"))
		assert.NotNil(t, billingFd.FindMessage("billing.v1.Invoice"), "Invoice is a dependency and should be kept")

		// Check common.proto
		commonFd := fdMap["common.proto"]
		require.NotNil(t, commonFd)
		assert.NotNil(t, commonFd.FindMessage("common.v1.User"), "User is a dependency of Invoice and should be kept")
		assert.Nil(t, commonFd.FindEnum("common.v1.Status"), "Status is not a dependency of the kept methods and should be trimmed")
	})
}

func Test_TrimMulti_MultiServiceSingleFile(t *testing.T) {
	// --- Test Fixtures: Load protos from the user's `example/muit` directory ---
	searchRoot := "example/muit"
	if _, err := os.Stat(searchRoot); os.IsNotExist(err) {
		t.Fatalf("Test directory %s not found. Make sure you run tests from the project root.", searchRoot)
	}

	protoContents, err := LoadProtos([]string{searchRoot})
	require.NoError(t, err)
	require.NotEmpty(t, protoContents)

	entryFile := "api/v1/commerce_service.proto"
	methodsToKeep := []string{
		"CommerceService.CreateUser",
		"TestService.TestMethod",
	}

	// --- Execution ---
	var trimmedFiles map[string]string
	_ = captureOutput(t, func() {
		trimmedFiles, err = TrimMulti([]string{entryFile}, methodsToKeep, protoContents)
	})

	// --- Assertions ---
	require.NoError(t, err)

	// 新的依赖分析:
	// CreateUser -> User, Order
	// TestMethod -> GetRequest, User
	// ---
	// 依赖 User -> user.proto, profile.proto, base.proto
	// 依赖 Order -> order.proto, item.proto, money.proto, base.proto, user.proto
	// 依赖 item.proto -> product.proto
	// 依赖 GetRequest -> common_messages.proto, base.proto
	// 合并后的文件列表 (9个):
	// commerce_service.proto, user.proto, profile.proto, order.proto, item.proto,
	// product.proto, common_messages.proto, base.proto, money.proto
	require.Len(t, trimmedFiles, 9, "Should keep all files related to User, Order, and GetRequest")
	assert.Contains(t, trimmedFiles, "api/v1/commerce_service.proto")
	assert.Contains(t, trimmedFiles, "services/user/user.proto")
	assert.Contains(t, trimmedFiles, "services/order/order.proto")
	assert.Contains(t, trimmedFiles, "services/product/product.proto")
	assert.Contains(t, trimmedFiles, "api/v1/common_messages.proto")
	assert.NotContains(t, trimmedFiles, "services/product/review.proto", "Review message is not a dependency and should be trimmed")

	// --- Deeper check of the trimmed content ---
	parser := protoparse.Parser{Accessor: protoparse.FileContentsFromMap(trimmedFiles)}
	fds, err := parser.ParseFiles(entryFile)
	require.NoError(t, err, "Trimmed files should be valid and parsable")

	var entryFd *desc.FileDescriptor
	for _, fd := range fds {
		if fd.GetName() == entryFile {
			entryFd = fd
			break
		}
	}
	require.NotNil(t, entryFd, "Entry file descriptor not found in parsed results")

	// =================================================================
	//  关键修复：检查更新后的方法签名
	// =================================================================
	commerceSvc := entryFd.FindService("api.v1.CommerceService")
	require.NotNil(t, commerceSvc)
	createUserMethod := commerceSvc.FindMethodByName("CreateUser")
	require.NotNil(t, createUserMethod)

	// 验证 CreateUser 的输入和输出类型
	assert.Equal(t, "services.user.User", createUserMethod.GetInputType().GetFullyQualifiedName(), "Input for CreateUser should be User")
	assert.Equal(t, "services.order.Order", createUserMethod.GetOutputType().GetFullyQualifiedName(), "Output for CreateUser should now be Order")

	testSvc := entryFd.FindService("api.v1.TestService")
	require.NotNil(t, testSvc)
	testMethod := testSvc.FindMethodByName("TestMethod")
	require.NotNil(t, testMethod)

	// 验证 TestMethod 的输入和输出类型
	assert.Equal(t, "api.v1.GetRequest", testMethod.GetInputType().GetFullyQualifiedName(), "Input for TestMethod should now be GetRequest")
	assert.Equal(t, "services.user.User", testMethod.GetOutputType().GetFullyQualifiedName(), "Output for TestMethod should be User")
}

// captureOutput temporarily redirects stdout to a buffer to keep test logs clean.
func captureOutput(t *testing.T, f func()) string {
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = w
	log.SetOutput(w)

	f()

	err = w.Close()
	require.NoError(t, err)

	os.Stdout = old
	log.SetOutput(old)

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)

	return buf.String()
}
