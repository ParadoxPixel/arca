FRONTEND := internal/frontend
BINARY := bin/arca

.PHONY: dev build frontend test fmt deploy clean

dev:
	cd $(FRONTEND) && pnpm install && pnpm dev

frontend:
	cd $(FRONTEND) && pnpm install && pnpm build

build: frontend
	go build -o $(BINARY) ./cmd/arca

test:
	go test ./...
	cd $(FRONTEND) && pnpm typecheck

fmt:
	gofmt -w ./cmd ./internal

# deploy.sh and deploy/ are deployment-specific and stay out of the repository.
# docs/deploying.md covers what they contain.
deploy:
	@test -x ./deploy.sh || { echo "no ./deploy.sh here, see docs/deploying.md"; exit 1; }
	./deploy.sh

clean:
	rm -rf $(BINARY) build $(FRONTEND)/dist
