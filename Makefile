gen:
	protoc --go_out=. --go_opt=paths=source_relative trimpbtest.proto
	protoc --descriptor_set_out=trimpbtest.pbdesc --include_imports trimpbtest.proto

build:
	cd cmd/trimpb && go build -o ../../trimpb
