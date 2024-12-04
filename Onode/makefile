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

# Variables
GO := go
ONODE := Onode.go

# Define all nodes with their attributes
# Format: NAME IP TYPE
NODES := \
S1 10.0.0.10 CS \
O1 10.0.0.1 NODE \
O2 10.0.1.1 NODE \
O3 10.0.3.2 POP \
O4 10.0.5.2 NODE \
O5 10.0.7.2 NODE \
O6 10.0.11.2 POP \
O7 10.0.10.2 POP \
O8 10.0.15.2 POP \
n12 10.0.16.2 NODE \
n13 10.0.17.2 NODE \
n14 10.0.19.2 NODE \
n15 10.0.21.2 NODE

# Default target
.PHONY: all clean S1 O1 O2 O3 O4 O5 O6 O7 O8 n12 n13 n14 n15

all: S1 O1 O2 O3 O4 O5 O6 O7 O8 n12 n13 n14 n15

# Explicit targets for each node
S1:
$(GO) run $(ONODE) -name S1 -ip 10.0.0.10 -type CS -verbose false

S2:
$(GO) run $(ONODE) -name S2 -ip 10.0.26.10 -type CS -verbose false

O1:
$(GO) run $(ONODE) -name O1 -ip 10.0.0.1 -type NODE -verbose false

O2:
$(GO) run $(ONODE) -name O2 -ip 10.0.1.1 -type NODE -verbose false

O3:
$(GO) run $(ONODE) -name O3 -ip 10.0.3.2 -type NODE -verbose false

O4:
$(GO) run $(ONODE) -name O4 -ip 10.0.5.2 -type NODE -verbose false

O5:
$(GO) run $(ONODE) -name O5 -ip 10.0.7.2 -type NODE -verbose false

O6: 
$(GO) run $(ONODE) -name O6 -ip 10.0.11.2 -type POP -verbose false

O7:
$(GO) run $(ONODE) -name O7 -ip 10.0.10.2 -type POP -verbose false

O8:
$(GO) run $(ONODE) -name O8 -ip 10.0.15.2 -type POP -verbose false

O1v:
$(GO) run $(ONODE) -name O1 -ip 10.0.0.1 -type NODE -verbose true

O2v:
$(GO) run $(ONODE) -name O2 -ip 10.0.1.1 -type NODE -verbose true

O3v:
$(GO) run $(ONODE) -name O3 -ip 10.0.3.2 -type NODE -verbose true

O4v:
$(GO) run $(ONODE) -name O4 -ip 10.0.5.2 -type NODE -verbose true

O5v:
$(GO) run $(ONODE) -name O5 -ip 10.0.7.2 -type NODE -verbose true

O6v:
$(GO) run $(ONODE) -name O6 -ip 10.0.11.2 -type POP -verbose true

O7v:
$(GO) run $(ONODE) -name O7 -ip 10.0.10.2 -type POP -verbose true

O8v:
$(GO) run $(ONODE) -name O8 -ip 10.0.15.2 -type POP -verbose true

# Clean target (optional, doesn't do much in this case but placeholder for future extensions)
clean:
@echo "Clean up completed."

