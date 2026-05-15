module github.com/synadia-io/orbit.go/microext

go 1.25.0

require (
	github.com/google/jsonschema-go v0.4.3
	github.com/nats-io/nats.go v1.50.0
)

require (
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
)

replace github.com/nats-io/nats.go => ../../nats.go
