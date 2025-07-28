package trimpb

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/desc/protoparse"
	"github.com/jhump/protoreflect/desc/protoprint"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

type trimmer struct {
	requiredMessages  map[protoreflect.FullName]struct{}
	requiredEnums     map[protoreflect.FullName]struct{}
	entryPointMethods []*desc.MethodDescriptor
	filesToTrim       map[string]*desc.FileDescriptor
}

func newTrimmer() *trimmer {
	return &trimmer{
		requiredMessages: make(map[protoreflect.FullName]struct{}),
		requiredEnums:    make(map[protoreflect.FullName]struct{}),
		filesToTrim:      make(map[string]*desc.FileDescriptor),
	}
}

func TrimMulti(entryProtoFiles []string, methodNames []string, importPaths []string, protoContents map[string]string) (map[string]string, error) {
	parser := protoparse.Parser{
		Accessor:              protoparse.FileContentsFromMap(protoContents),
		IncludeSourceCodeInfo: true, // Preserve source code info for comments
		ImportPaths:           importPaths,
	}

	entryFds, err := parser.ParseFiles(entryProtoFiles...)
	if err != nil {
		return nil, fmt.Errorf("failed to parse proto files from map: %w", err)
	}

	allFds := collectAllDependencies(entryFds)

	trimmedResults, err := runTrim(entryFds, methodNames, allFds)
	if err != nil {
		return nil, err
	}

	finalResults := make(map[string]string)
	for trimmedPath, content := range trimmedResults {
		realPath := findRealPath(trimmedPath, importPaths, protoContents)
		finalResults[realPath] = content
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

func runTrim(entryFileDescs []*desc.FileDescriptor, methodNames []string, fds []*desc.FileDescriptor) (map[string]string, error) {
	if len(entryFileDescs) == 0 {
		return nil, fmt.Errorf("no entry proto files were parsed successfully")
	}

	t := newTrimmer()

	if len(methodNames) == 0 {
		for _, fd := range entryFileDescs {
			for _, service := range fd.GetServices() {
				t.entryPointMethods = append(t.entryPointMethods, service.GetMethods()...)
			}
		}
	} else {
		for _, methodName := range methodNames {
			methods, err := findMethods(methodName, entryFileDescs, fds)
			if err != nil {
				return nil, err
			}
			t.entryPointMethods = append(t.entryPointMethods, methods...)
		}
	}

	for _, method := range t.entryPointMethods {
		t.collectDependencies(method.GetInputType())
		t.collectDependencies(method.GetOutputType())
	}

	if len(t.entryPointMethods) == 0 && len(methodNames) > 0 {
		fmt.Println("Warning: No methods matched the given names, no files will be trimmed.")
		return make(map[string]string), nil
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

func findMethods(methodName string, entryFiles []*desc.FileDescriptor, allFiles []*desc.FileDescriptor) ([]*desc.MethodDescriptor, error) {
	dotCount := strings.Count(methodName, ".")

	if dotCount >= 2 { // Fully qualified name (e.g., package.Service.Method)
		for _, fd := range allFiles {
			if d := fd.FindSymbol(methodName); d != nil {
				if md, ok := d.(*desc.MethodDescriptor); ok {
					return []*desc.MethodDescriptor{md}, nil
				}
			}
		}
	} else if dotCount == 1 { // Service.Method
		parts := strings.Split(methodName, ".")
		serviceName, simpleMethodName := parts[0], parts[1]
		for _, entryFile := range entryFiles {
			for _, service := range entryFile.GetServices() {
				if service.GetName() == serviceName {
					if method := service.FindMethodByName(simpleMethodName); method != nil {
						return []*desc.MethodDescriptor{method}, nil
					}
				}
			}
		}
	} else { // Partial method name match
		var foundMethods []*desc.MethodDescriptor
		for _, entryFile := range entryFiles {
			for _, service := range entryFile.GetServices() {
				for _, method := range service.GetMethods() {
					if strings.Contains(method.GetName(), methodName) {
						foundMethods = append(foundMethods, method)
					}
				}
			}
		}
		if len(foundMethods) > 0 {
			fmt.Printf("Found %d methods matching '%s'\n", len(foundMethods), methodName)
			return foundMethods, nil
		}
	}

	return nil, fmt.Errorf("method matching '%s' not found in any of the provided entry files or their imports", methodName)
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

	// Maps: original desc.Descriptor -> new Proto file index
	origMsgToNewIndex := make(map[*desc.MessageDescriptor]int)
	origEnumToNewIndex := make(map[*desc.EnumDescriptor]int)
	origServiceToNewIndex := make(map[*desc.ServiceDescriptor]int)
	origMethodToNewIndex := make(map[*desc.ServiceDescriptor]map[*desc.MethodDescriptor]int)

	// Filter and collect messages, build index map
	for _, msg := range originalFd.GetMessageTypes() {
		if _, ok := t.requiredMessages[msg.Unwrap().FullName()]; ok {
			origMsgToNewIndex[msg] = len(newProto.MessageType)
			newProto.MessageType = append(newProto.MessageType, msg.AsDescriptorProto())
		}
	}

	// Filter and collect enums, build index map
	for _, enum := range originalFd.GetEnumTypes() {
		if _, ok := t.requiredEnums[enum.Unwrap().FullName()]; ok {
			origEnumToNewIndex[enum] = len(newProto.EnumType)
			newProto.EnumType = append(newProto.EnumType, enum.AsEnumDescriptorProto())
		}
	}

	// Filter and collect services and methods, build index map
	methodsByService := make(map[protoreflect.FullName][]*desc.MethodDescriptor)
	for _, method := range t.entryPointMethods {
		if method.GetFile().GetName() == originalFd.GetName() {
			service := method.GetService()
			methodsByService[service.Unwrap().FullName()] = append(methodsByService[service.Unwrap().FullName()], method)
		}
	}

	for _, svc := range originalFd.GetServices() {
		if methods, ok := methodsByService[svc.Unwrap().FullName()]; ok {
			origServiceToNewIndex[svc] = len(newProto.Service)
			newSvcProto := &descriptorpb.ServiceDescriptorProto{
				Name:    stringPtr(svc.GetName()),
				Options: svc.GetServiceOptions(),
			}
			methodMap := make(map[*desc.MethodDescriptor]int)
			for _, method := range methods {
				methodMap[method] = len(newSvcProto.Method)
				newSvcProto.Method = append(newSvcProto.Method, method.AsMethodDescriptorProto())
			}
			newProto.Service = append(newProto.Service, newSvcProto)
			origMethodToNewIndex[svc] = methodMap
		}
	}

	// Process dependencies
	for _, dep := range originalFd.GetDependencies() {
		if _, ok := t.filesToTrim[dep.GetName()]; ok {
			newProto.Dependency = append(newProto.Dependency, dep.GetName())
		}
	}

	// Rebuild SourceCodeInfo and re-index paths
	originalFileProto := originalFd.AsFileDescriptorProto()
	if originalFileProto != nil && originalFileProto.GetSourceCodeInfo() != nil {
		newSourceCodeInfo := &descriptorpb.SourceCodeInfo{}
		originalLocations := originalFileProto.GetSourceCodeInfo().GetLocation()

		for _, loc := range originalLocations {
			path := loc.GetPath()
			newPath := make([]int32, len(path))
			copy(newPath, path)

			kept := false
			if len(path) == 0 { // File-level comment
				kept = true
			} else {
				switch path[0] {
				case 4: // Top-level message (descriptor_proto.MessageType)
					if len(path) >= 2 {
						originalMsgIndex := int(path[1])
						if originalMsgIndex < len(originalFd.GetMessageTypes()) {
							originalMsg := originalFd.GetMessageTypes()[originalMsgIndex]
							if newIndex, ok := origMsgToNewIndex[originalMsg]; ok {
								newPath[1] = int32(newIndex)
								kept = true // Keep message-level comments and potentially nested elements if complex logic is added
							}
						}
					}
				case 5: // Top-level enum (descriptor_proto.EnumType)
					if len(path) >= 2 {
						originalEnumIndex := int(path[1])
						if originalEnumIndex < len(originalFd.GetEnumTypes()) {
							originalEnum := originalFd.GetEnumTypes()[originalEnumIndex]
							if newIndex, ok := origEnumToNewIndex[originalEnum]; ok {
								newPath[1] = int32(newIndex)
								kept = true // Keep enum-level comments
							}
						}
					}
				case 6: // Top-level service (descriptor_proto.Service)
					if len(path) >= 2 {
						originalServiceIndex := int(path[1])
						if originalServiceIndex < len(originalFd.GetServices()) {
							originalSvc := originalFd.GetServices()[originalServiceIndex]
							if newIndex, ok := origServiceToNewIndex[originalSvc]; ok {
								newPath[1] = int32(newIndex)
								if len(path) >= 4 && path[2] == 2 { // Method inside service
									originalMethodIndex := int(path[3])
									if originalMethodIndex < len(originalSvc.GetMethods()) {
										originalMethod := originalSvc.GetMethods()[originalMethodIndex]
										if methodMap, mapOK := origMethodToNewIndex[originalSvc]; mapOK {
											if newMethodIndex, methodOK := methodMap[originalMethod]; methodOK {
												newPath[3] = int32(newMethodIndex)
												kept = true
											}
										}
									}
								} else { // Service comments
									kept = true
								}
							}
						}
					}
				case 1: // Package declaration
					kept = true
				}
			}

			if kept {
				newLoc := proto.Clone(loc).(*descriptorpb.SourceCodeInfo_Location)
				newLoc.Path = newPath
				newSourceCodeInfo.Location = append(newSourceCodeInfo.Location, newLoc)
			}
		}
		newProto.SourceCodeInfo = newSourceCodeInfo
	}

	return newProto
}

func findRealPath(path string, importPaths []string, protoContents map[string]string) string {
	for _, importPath := range importPaths {
		joinedPath := filepath.Clean(filepath.Join(importPath, path))
		if _, ok := protoContents[joinedPath]; ok {
			return joinedPath
		}
	}
	if _, ok := protoContents[path]; ok {
		return path
	}
	return path
}

func stringPtr(s string) *string {
	return &s
}
