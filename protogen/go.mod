// Module protogen holds the generated protobuf and gRPC stubs shared by
// every Go component.
//
// It is its own module because the backend and the client both need these
// types, and generating a copy into each one is fine right up until a
// binary links both — which the desktop app now does, since it runs the
// control plane in-process. Protobuf keeps a single global registry keyed on
// message full name, so two generated copies of the same .proto in one
// process is a panic at init, before main runs and with nothing useful to
// report to the user.
module github.com/jitheshsisodiya/Ed-s/protogen

go 1.25.0

require (
	google.golang.org/grpc v1.76.0
	google.golang.org/protobuf v1.36.11
)

require (
	golang.org/x/net v0.42.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/text v0.27.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250804133106-a7a43d27e69b // indirect
)
