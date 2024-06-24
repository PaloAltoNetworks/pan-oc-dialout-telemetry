all:
	protoc --proto_path=proto -I proto/*.proto --go_out=. --go-grpc_out=.
	go build

clean:
	rm proto/*.go
