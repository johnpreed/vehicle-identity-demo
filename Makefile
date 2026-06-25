COMPOSE = docker compose -f deploy/docker-compose.yml
INFRA = docker compose -f deploy/docker-compose.infra.yml

.PHONY: up down test logs build ps infra infra-down

up: ## Build and start the full stack
	$(COMPOSE) up --build -d
	@echo "consumer-web: http://localhost:5173   staff-web: http://localhost:5174"

build: ## Build images only
	$(COMPOSE) build

down: ## Stop and remove containers + volumes
	$(COMPOSE) down -v

infra: ## Start infra only (Postgres + web apps) for host-side debugging in VS Code
	$(INFRA) up --build -d
	@echo ""
	@echo "Infra is up:"
	@echo "  consumer-web : http://localhost:5173"
	@echo "  staff-web    : http://localhost:5174"
	@echo "  postgres     : localhost:5432"
	@echo ""
	@echo "Next: debug the Go services from VS Code (.vscode/launch.json -> 'Debug all services')."
	@echo "They listen on: identity :8081  vehicle :8082  audit :8083  simulated-vehicle :8084"

infra-down: ## Stop the infra-only stack (keeps the pgdata volume)
	$(INFRA) down

logs: ## Tail logs from all services
	$(COMPOSE) logs -f

ps: ## Show container status
	$(COMPOSE) ps

test: ## Run Go unit tests
	go test ./...