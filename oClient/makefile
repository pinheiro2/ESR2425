# Variables
GO := go
CLIENT := client.go
POP_IP := 10.0.11.2
STREAM := $(or $(stream), stream1) # Default value is 'stream1'
CONFIG := pops.json

# Targets
.PHONY: all client clean

# Run all
all: client

# Run the Client
client:
	$(GO) run $(CLIENT) -stream $(STREAM) -config $(CONFIG)

# Explicit targets for different clients
client1:
	$(MAKE) client STREAM=stream1 CONFIG=pops.json

client2:
	$(MAKE) client STREAM=stream2 CONFIG=pops.json

client3:
	$(MAKE) client STREAM=stream3 CONFIG=pops.json

client7:
	$(MAKE) client STREAM=stream1 CONFIG=pops_O7.json

client8:
	$(MAKE) client STREAM=stream1 CONFIG=pops_O8.json
# Clean target (optional, doesn't do much in this case but placeholder for future extensions)
clean:
	@echo "Clean up completed."
