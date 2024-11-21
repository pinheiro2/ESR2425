# Variables
GO := go
CLIENT := client.go
POP_IP := 10.0.3.2
STREAM := $(or $(stream), stream1) # Default value is 'stream1'

# Targets
.PHONY: all client clean

# Run all
all: client

# Run the Client
client:
	$(GO) run $(CLIENT) -pop-ip $(POP_IP) -stream $(STREAM)

# Clean target (optional, doesn't do much in this case but placeholder for future extensions)
clean:
	@echo "Clean up completed."
