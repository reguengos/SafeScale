GO?=go
EXEC:=brokerd

.PHONY:	brokerd services clean

all:	brokerd 

vet:
	@$(GO) vet ./...

brokerd: services main.go commands/*.go ../utils/*.go ../broker.pb.go 
	@$(GO) build -o $(EXEC)

services:
	@(cd services && $(MAKE))

clean:
	@(cd services && $(MAKE) $@)
	@$(RM) $(EXEC) || true
	@$(RM) ./mocks/*.go || true
