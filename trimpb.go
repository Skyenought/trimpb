package trimpb

import (
	"fmt"
	"strings"

	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/desc/protoparse"
	"github.com/jhump/protoreflect/desc/protoprint"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// trimmer manages the state of the dependency collection process.
type trimmer struct {
	requiredMessages  map[protoreflect.FullName]struct{}
	requiredEnums     map[protoreflect.FullName]struct{}
	entryPointMethods []*desc.MethodDescriptor
	filesToTrim       map[string]*desc.FileDescriptor
}

// newTrimmer creates a new instance of a trimmer.
func newTrimmer() *trimmer {
	return &trimmer{
		requiredMessages: make(map[protoreflect.FullName]struct{}),
		requiredEnums:    make(map[protoreflect.FullName]struct{}),
		filesToTrim:      make(map[string]*desc.FileDescriptor),
	}
}

// Trim parses a set of in-memory proto files from a single entry point, identifies the specified RPC methods
// and their transitive dependencies, and returns new in-memory proto files containing only the necessary definitions.
// It is a convenience wrapper around TrimMulti.
//
// Parameters:
//   - entryProtoFile: The path to the main .proto file to begin trimming from.
//   - methodNames: A slice of RPC method names to keep.
//   - protoContents: A map where keys are proto file paths and values are the string contents of those files.
//
// Returns:
//   - A map where keys are the original file paths and values are the new, trimmed string contents of those files.
func Trim(entryProtoFile string, methodNames []string, protoContents map[string]string) (map[string]string, error) {
	return TrimMulti([]string{entryProtoFile}, methodNames, protoContents)
}

// TrimMulti parses a set of in-memory proto files from multiple entry points, identifies the specified RPC methods
// and their transitive dependencies, and returns new in-memory proto files containing only the necessary definitions.
// This function operates purely on in-memory data and is ideal for testing and decoupled environments.
//
// Parameters:
//   - entryProtoFiles: A slice of paths to the main .proto files to begin trimming from.
//   - methodNames: A slice of RPC method names to keep.
//   - protoContents: A map where keys are proto file paths (relative to a virtual root) and values are the string contents of those files.
//
// Returns:
//   - A map where keys are the original file paths and values are the new, trimmed string contents of those files.
func TrimMulti(entryProtoFiles []string, methodNames []string, protoContents map[string]string) (map[string]string, error) {
	// 1. Prepare list of files to parse from the map keys.
	filesToParse := make([]string, 0, len(protoContents))
	for path := range protoContents {
		filesToParse = append(filesToParse, path)
	}

	// 2. Parse all provided .proto files from memory using an Accessor.
	parser := protoparse.Parser{
		Accessor:              protoparse.FileContentsFromMap(protoContents),
		IncludeSourceCodeInfo: true,
	}

	// When parsing from a map, ParseFiles *does* return all descriptors, because we pass all file names.
	fds, err := parser.ParseFiles(filesToParse...)
	if err != nil {
		return nil, fmt.Errorf("failed to parse proto files from map: %w", err)
	}

	return runTrim(entryProtoFiles, methodNames, fds)
}

// TrimWithImportPaths trims proto files by directly using file system paths, similar to how `protoc -I` works.
// This function is useful for command-line tools and scenarios where direct file system access is preferred.
//
// Parameters:
//   - entryProtoFiles: A slice of paths to the main .proto files, relative to one of the importPaths.
//   - methodNames: A slice of RPC method names to keep.
//   - importPaths: A slice of directories to search for .proto files (like protoc's -I flag).
//
// Returns:
//   - A map where keys are the original file paths and values are the new, trimmed string contents of those files.
func TrimWithImportPaths(entryProtoFiles []string, methodNames []string, importPaths []string) (map[string]string, error) {
	// 1. Parse proto files by letting the parser read directly from the file system.
	parser := protoparse.Parser{
		ImportPaths:           importPaths,
		IncludeSourceCodeInfo: true,
	}

	// THIS IS THE KEY: ParseFiles only returns descriptors for the files explicitly listed.
	entryFds, err := parser.ParseFiles(entryProtoFiles...)
	if err != nil {
		return nil, fmt.Errorf("failed to parse proto files from import paths %v: %w", importPaths, err)
	}

	// file descriptors that the parser loaded.
	allFds := collectAllDependencies(entryFds)

	// The rest of the logic is the same, but now it operates on the complete set of files.
	return runTrim(entryProtoFiles, methodNames, allFds)
}

// collectAllDependencies performs a breadth-first search to gather all transitive
// file dependencies from a given set of entry-point file descriptors.
func collectAllDependencies(entryFds []*desc.FileDescriptor) []*desc.FileDescriptor {
	allFdsMap := make(map[string]*desc.FileDescriptor)
	queue := make([]*desc.FileDescriptor, len(entryFds))
	copy(queue, entryFds)

	for len(queue) > 0 {
		fd := queue[0]
		queue = queue[1:]

		if _, visited := allFdsMap[fd.GetName()]; visited {
			continue
		}
		allFdsMap[fd.GetName()] = fd

		for _, dep := range fd.GetDependencies() {
			queue = append(queue, dep)
		}
	}

	result := make([]*desc.FileDescriptor, 0, len(allFdsMap))
	for _, fd := range allFdsMap {
		result = append(result, fd)
	}
	return result
}

// runTrim contains the core trimming logic, shared by both TrimMulti and TrimWithImportPaths.
func runTrim(entryProtoFiles []string, methodNames []string, fds []*desc.FileDescriptor) (map[string]string, error) {
	fmt.Printf("Successfully parsed %d file(s) (including dependencies).\n", len(fds))

	// 3. Find file descriptors for all entry point files.
	entryFileMap := make(map[string]*desc.FileDescriptor)
	for _, entryPath := range entryProtoFiles {
		var found bool
		for _, fd := range fds {
			if fd.GetName() == entryPath {
				entryFileMap[entryPath] = fd
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("entry proto file '%s' not found in parsed files", entryPath)
		}
	}
	entryFileDescs := make([]*desc.FileDescriptor, 0, len(entryFileMap))
	for _, fd := range entryFileMap {
		entryFileDescs = append(entryFileDescs, fd)
	}

	t := newTrimmer()

	// 4. Find all specified entry point methods.
	fmt.Printf("Searching for %d entry point method(s)...\n", len(methodNames))
	for _, methodName := range methodNames {
		md, err := findMethod(methodName, entryFileDescs, fds)
		if err != nil {
			return nil, err
		}
		fmt.Printf("  - Found method '%s' in file '%s'\n", md.GetFullyQualifiedName(), md.GetFile().GetName())
		t.entryPointMethods = append(t.entryPointMethods, md)
	}

	// 5. Recursively collect all dependencies for all entry point methods.
	fmt.Println("Collecting dependencies...")
	for _, method := range t.entryPointMethods {
		t.collectDependencies(method.GetInputType())
		t.collectDependencies(method.GetOutputType())
	}

	// 6. Determine which files are affected (contain required definitions).
	for _, fd := range fds {
		if t.isFileRequired(fd) {
			t.filesToTrim[fd.GetName()] = fd
		}
	}
	fmt.Printf("Found %d files containing required definitions.\n", len(t.filesToTrim))

	// 7. Filter each required file descriptor to create a new, trimmed version.
	var filteredFileProtos []*descriptorpb.FileDescriptorProto
	for _, originalFd := range t.filesToTrim {
		newProto := t.filterFileDescriptor(originalFd)
		filteredFileProtos = append(filteredFileProtos, newProto)
	}

	fileSet := &descriptorpb.FileDescriptorSet{File: filteredFileProtos}
	newFds, err := desc.CreateFileDescriptorsFromSet(fileSet)
	if err != nil {
		return nil, fmt.Errorf("failed to create new descriptors from filtered set: %w", err)
	}

	// 8. Print the new, trimmed .proto files to a map of strings.
	p := &protoprint.Printer{}
	result := make(map[string]string)
	for path, newFd := range newFds {
		str, err := p.PrintProtoToString(newFd)
		if err != nil {
			return nil, fmt.Errorf("failed to print new proto file %s: %w", path, err)
		}
		result[path] = str
	}

	fmt.Println("\nDone!")
	return result, nil
}

// findMethod finds a method descriptor. It intelligently distinguishes
// between short names (Service.Method) and fully-qualified names (pkg.Service.Method).
func findMethod(methodName string, entryFiles []*desc.FileDescriptor, allFiles []*desc.FileDescriptor) (*desc.MethodDescriptor, error) {
	dotCount := strings.Count(methodName, ".")

	// Case 1: Assumed fully-qualified name, e.g., "package.v1.Service.Method" (dotCount >= 2)
	if dotCount >= 2 {
		for _, fd := range allFiles {
			if d := fd.FindSymbol(methodName); d != nil {
				if md, ok := d.(*desc.MethodDescriptor); ok {
					return md, nil
				}
			}
		}
	} else if dotCount == 1 { // Case 2: Assumed short name, e.g., "Service.Method", search in all entry files
		parts := strings.Split(methodName, ".")
		serviceName, simpleMethodName := parts[0], parts[1]

		for _, entryFile := range entryFiles {
			for _, service := range entryFile.GetServices() {
				if service.GetName() == serviceName {
					if method := service.FindMethodByName(simpleMethodName); method != nil {
						return method, nil
					}
				}
			}
		}
	} else { // Case 3: Invalid format
		return nil, fmt.Errorf("invalid method name format: '%s'. Expected 'Service.Method' or 'package.Service.Method'", methodName)
	}

	// If all attempts fail
	return nil, fmt.Errorf("method '%s' not found in any of the provided entry files or their imports", methodName)
}

// collectDependencies recursively finds all message and enum types required by a message.
func (t *trimmer) collectDependencies(md *desc.MessageDescriptor) {
	if _, ok := t.requiredMessages[md.Unwrap().FullName()]; ok {
		return // Already processed
	}
	t.requiredMessages[md.Unwrap().FullName()] = struct{}{}
	for _, field := range md.GetFields() {
		if field.GetMessageType() != nil {
			t.collectDependencies(field.GetMessageType())
		}
		if field.GetEnumType() != nil {
			t.requiredEnums[field.GetEnumType().Unwrap().FullName()] = struct{}{}
		}
	}
}

// isFileRequired checks if a file descriptor contains any definitions that we need to keep.
func (t *trimmer) isFileRequired(fd *desc.FileDescriptor) bool {
	// Check if this file contains one of our entry point methods.
	for _, m := range t.entryPointMethods {
		if fd.GetFile().GetName() == m.GetFile().GetName() {
			return true
		}
	}
	// Check if it contains any required messages.
	for _, mtd := range fd.GetMessageTypes() {
		if _, ok := t.requiredMessages[mtd.Unwrap().FullName()]; ok {
			return true
		}
	}
	// Check if it contains any required enums.
	for _, etd := range fd.GetEnumTypes() {
		if _, ok := t.requiredEnums[etd.Unwrap().FullName()]; ok {
			return true
		}
	}
	return false
}

// filterFileDescriptor creates a new, trimmed file descriptor proto from an original one.
func (t *trimmer) filterFileDescriptor(originalFd *desc.FileDescriptor) *descriptorpb.FileDescriptorProto {
	newProto := &descriptorpb.FileDescriptorProto{
		Name:    stringPtr(originalFd.GetName()), // Keep original name
		Package: stringPtr(originalFd.GetPackage()),
		Options: originalFd.GetFileOptions(),
	}

	if originalFd.IsProto3() {
		newProto.Syntax = stringPtr("proto3")
	} else {
		newProto.Syntax = stringPtr("proto2")
	}

	// Keep only dependencies that are also being trimmed.
	for _, dep := range originalFd.GetDependencies() {
		if _, ok := t.filesToTrim[dep.GetName()]; ok {
			newProto.Dependency = append(newProto.Dependency, dep.GetName())
		}
	}

	// Keep only the required message types.
	for _, msg := range originalFd.GetMessageTypes() {
		if _, ok := t.requiredMessages[msg.Unwrap().FullName()]; ok {
			newProto.MessageType = append(newProto.MessageType, msg.AsDescriptorProto())
		}
	}

	// Keep only the required enum types.
	for _, enum := range originalFd.GetEnumTypes() {
		if _, ok := t.requiredEnums[enum.Unwrap().FullName()]; ok {
			newProto.EnumType = append(newProto.EnumType, enum.AsEnumDescriptorProto())
		}
	}

	// Group required methods by service for the current file.
	methodsByService := make(map[protoreflect.FullName][]*desc.MethodDescriptor)
	for _, method := range t.entryPointMethods {
		if method.GetFile().GetName() == originalFd.GetName() {
			service := method.GetService()
			fullName := service.Unwrap().FullName()
			methodsByService[fullName] = append(methodsByService[fullName], method)
		}
	}

	// Reconstruct services with only the required methods.
	for _, svc := range originalFd.GetServices() {
		if methods, ok := methodsByService[svc.Unwrap().FullName()]; ok {
			newSvcProto := &descriptorpb.ServiceDescriptorProto{
				Name:    stringPtr(svc.GetName()),
				Options: svc.GetServiceOptions(),
			}
			for _, method := range methods {
				newSvcProto.Method = append(newSvcProto.Method, method.AsMethodDescriptorProto())
			}
			newProto.Service = append(newProto.Service, newSvcProto)
		}
	}

	return newProto
}

func stringPtr(s string) *string {
	return &s
}
