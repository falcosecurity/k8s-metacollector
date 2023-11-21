# Test server

This folder contains a test server that sends a set of predefined JSON events when the Watch API is called. It could be useful for testing the integration with the Falco official plugin for example.

## Build and run

If you update the `metadata.proto` remember to rebuild the server.

```bash
go build .
./test_server --file <path_to_the_json_file>
```
