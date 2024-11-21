# Variables
GO := go
ONODE := Onode.go
CS_NAME := S1
CS_IP := 10.0.0.10
POP_NAME := O3
POP_IP := 10.0.3.2
NODE_NAME := O1
NODE_IP := 10.0.0.1

STREAM := stream1
# Targets
.PHONY: all cs pop client clean

# Run all
all: cs pop client

# Run the Content Server (CS)
cs:
	$(GO) run $(ONODE) -name $(CS_NAME) -ip $(CS_IP) -type CS

# Run the Point of Presence (POP)
node:
	$(GO) run $(ONODE) -name $(NODE_NAME) -ip $(NODE_IP) -type NODE


# Run the Point of Presence (POP)
pop:
	$(GO) run $(ONODE) -name $(POP_NAME) -ip $(POP_IP) -type POP

# Clean target (optional, doesn't do much in this case but placeholder for future extensions)
clean:
	@echo "Clean up completed."
