package trimpb

import (
	"fmt"
	"path/filepath"
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

// Trim is a convenience wrapper around TrimMulti.
func Trim(entryProtoFile string, methodNames []string, protoContents map[string]string) (map[string]string, error) {
	return TrimMulti([]string{entryProtoFile}, methodNames, protoContents)
}

// TrimMulti operates purely on in-memory data, using a map of file paths to their contents.
// It does not access the file system.
func TrimMulti(entryProtoFiles []string, methodNames []string, protoContents map[string]string) (map[string]string, error) {
	if len(protoContents) == 0 {
		return nil, fmt.Errorf("protoContents map cannot be empty")
	}

	// --- 自动修复逻辑开始 ---
	allPaths := make([]string, 0, len(protoContents))
	for path := range protoContents {
		allPaths = append(allPaths, path)
	}

	// 1. 找到所有文件路径的最长公共前缀作为虚拟的 "import root"
	commonRoot := findLongestCommonPrefixPath(allPaths)
	if commonRoot != "" {
		// 确保公共根路径以分隔符结尾，以便正确地 TrimPrefix
		commonRoot += string(filepath.Separator)
		fmt.Printf("Auto-detected common import root: %s\n", commonRoot)
	}

	// 2. 重映射 protoContents 的 keys 和 entryProtoFiles 的路径
	remappedProtoContents := make(map[string]string, len(protoContents))
	for path, content := range protoContents {
		newKey := strings.TrimPrefix(path, commonRoot)
		remappedProtoContents[newKey] = content
	}

	remappedEntryFiles := make([]string, 0, len(entryProtoFiles))
	for _, entry := range entryProtoFiles {
		newEntry := strings.TrimPrefix(entry, commonRoot)
		remappedEntryFiles = append(remappedEntryFiles, newEntry)
	}
	// --- 自动修复逻辑结束 ---

	parser := protoparse.Parser{
		// 使用重映射后的 map 作为 accessor
		Accessor:              protoparse.FileContentsFromMap(remappedProtoContents),
		IncludeSourceCodeInfo: true,
	}

	fmt.Printf("Attempting to parse remapped entry files from memory: %v\n", remappedEntryFiles)

	// 使用重映射后的入口文件列表进行解析
	entryFds, err := parser.ParseFiles(remappedEntryFiles...)
	if err != nil {
		return nil, fmt.Errorf("failed to parse proto files from map: %w", err)
	}

	allFds := collectAllDependencies(entryFds)

	// 【重要】: 调用 runTrim 时，我们仍然使用原始的 entryProtoFiles 列表。
	// 这是为了确保最终返回给用户的 map 的 key 是他们最初提供的、完整的路径，符合用户预期。
	trimmedResults, err := runTrim(entryProtoFiles, methodNames, allFds)
	if err != nil {
		return nil, err
	}

	// 由于 runTrim 内部创建的 descriptor 会使用相对路径，我们需要将结果的 key 再次映射回原始的完整路径。
	finalResults := make(map[string]string)
	for trimmedPath, content := range trimmedResults {
		originalPath := commonRoot + trimmedPath
		// 检查原始路径是否存在，以防万一
		if _, ok := protoContents[originalPath]; ok {
			finalResults[originalPath] = content
		} else {
			// 作为一个回退，如果找不到原始路径，就使用裁剪后的路径
			// 这种情况理论上不应该发生
			finalResults[trimmedPath] = content
		}
	}

	return finalResults, nil
}
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

func runTrim(entryProtoFiles []string, methodNames []string, fds []*desc.FileDescriptor) (map[string]string, error) {
	fmt.Printf("Successfully parsed %d file(s) (including dependencies).\n", len(fds))

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

	fmt.Printf("Searching for %d entry point method(s)...\n", len(methodNames))
	for _, methodName := range methodNames {
		md, err := findMethod(methodName, entryFileDescs, fds)
		if err != nil {
			return nil, err
		}
		fmt.Printf("  - Found method '%s' in file '%s'\n", md.GetFullyQualifiedName(), md.GetFile().GetName())
		t.entryPointMethods = append(t.entryPointMethods, md)
	}

	fmt.Println("Collecting dependencies...")
	for _, method := range t.entryPointMethods {
		t.collectDependencies(method.GetInputType())
		t.collectDependencies(method.GetOutputType())
	}

	for _, fd := range fds {
		if t.isFileRequired(fd) {
			t.filesToTrim[fd.GetName()] = fd
		}
	}
	fmt.Printf("Found %d files containing required definitions.\n", len(t.filesToTrim))

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

func findMethod(methodName string, entryFiles []*desc.FileDescriptor, allFiles []*desc.FileDescriptor) (*desc.MethodDescriptor, error) {
	dotCount := strings.Count(methodName, ".")
	if dotCount >= 2 {
		for _, fd := range allFiles {
			if d := fd.FindSymbol(methodName); d != nil {
				if md, ok := d.(*desc.MethodDescriptor); ok {
					return md, nil
				}
			}
		}
	} else if dotCount == 1 {
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
	} else {
		return nil, fmt.Errorf("invalid method name format: '%s'. Expected 'Service.Method' or 'package.Service.Method'", methodName)
	}
	return nil, fmt.Errorf("method '%s' not found in any of the provided entry files or their imports", methodName)
}

func (t *trimmer) collectDependencies(md *desc.MessageDescriptor) {
	if _, ok := t.requiredMessages[md.Unwrap().FullName()]; ok {
		return
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

func (t *trimmer) isFileRequired(fd *desc.FileDescriptor) bool {
	for _, m := range t.entryPointMethods {
		if fd.GetFile().GetName() == m.GetFile().GetName() {
			return true
		}
	}
	for _, mtd := range fd.GetMessageTypes() {
		if _, ok := t.requiredMessages[mtd.Unwrap().FullName()]; ok {
			return true
		}
	}
	for _, etd := range fd.GetEnumTypes() {
		if _, ok := t.requiredEnums[etd.Unwrap().FullName()]; ok {
			return true
		}
	}
	return false
}

func (t *trimmer) filterFileDescriptor(originalFd *desc.FileDescriptor) *descriptorpb.FileDescriptorProto {
	newProto := &descriptorpb.FileDescriptorProto{
		Name:    stringPtr(originalFd.GetName()),
		Package: stringPtr(originalFd.GetPackage()),
		Options: originalFd.GetFileOptions(),
	}

	if originalFd.IsProto3() {
		newProto.Syntax = stringPtr("proto3")
	} else {
		newProto.Syntax = stringPtr("proto2")
	}

	for _, dep := range originalFd.GetDependencies() {
		if _, ok := t.filesToTrim[dep.GetName()]; ok {
			newProto.Dependency = append(newProto.Dependency, dep.GetName())
		}
	}

	for _, msg := range originalFd.GetMessageTypes() {
		if _, ok := t.requiredMessages[msg.Unwrap().FullName()]; ok {
			newProto.MessageType = append(newProto.MessageType, msg.AsDescriptorProto())
		}
	}

	for _, enum := range originalFd.GetEnumTypes() {
		if _, ok := t.requiredEnums[enum.Unwrap().FullName()]; ok {
			newProto.EnumType = append(newProto.EnumType, enum.AsEnumDescriptorProto())
		}
	}

	methodsByService := make(map[protoreflect.FullName][]*desc.MethodDescriptor)
	for _, method := range t.entryPointMethods {
		if method.GetFile().GetName() == originalFd.GetName() {
			service := method.GetService()
			fullName := service.Unwrap().FullName()
			methodsByService[fullName] = append(methodsByService[fullName], method)
		}
	}

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

// findLongestCommonPrefixPath 找到一组路径的最长公共目录前缀。
// 例如：["a/b/c", "a/b/d"] -> "a/b"
func findLongestCommonPrefixPath(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	if len(paths) == 1 {
		return filepath.Dir(paths[0])
	}

	// 按路径分隔符分割所有路径
	parts := make([][]string, len(paths))
	for i, path := range paths {
		parts[i] = strings.Split(path, string(filepath.Separator))
	}

	minLen := len(parts[0])
	for _, p := range parts {
		if len(p) < minLen {
			minLen = len(p)
		}
	}

	var commonPrefix []string
	for i := 0; i < minLen; i++ {
		firstPart := parts[0][i]
		allMatch := true
		for j := 1; j < len(parts); j++ {
			if parts[j][i] != firstPart {
				allMatch = false
				break
			}
		}
		if allMatch {
			commonPrefix = append(commonPrefix, firstPart)
		} else {
			break
		}
	}

	return strings.Join(commonPrefix, string(filepath.Separator))
}

func stringPtr(s string) *string {
	return &s
}
