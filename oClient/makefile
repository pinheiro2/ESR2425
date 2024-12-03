# Variables
GO := go
CLIENT := client.go
POP_IP := 10.0.11.2
STREAM := $(or $(stream), stream1) # Default value is 'stream1'

# Targets
.PHONY: all client clean

# Run all
all: client

# Run the Client
client:
	$(GO) run $(CLIENT) -stream $(STREAM) -config $(CONFIG)

# Explicit targets for different clients
client1:
	$(MAKE) client STREAM=stream1

client2:
	$(MAKE) client STREAM=stream1 CONFIG=pops_O7.json

client3:
	$(MAKE) client STREAM=stream3

client4:
	$(MAKE) client STREAM=stream2


# Clean target (optional, doesn't do much in this case but placeholder for future extensions)
clean:
	@echo "Clean up completed."
