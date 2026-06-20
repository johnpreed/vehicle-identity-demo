COMPOSE = docker compose -f deploy/docker-compose.yml

.PHONY: up down test logs build ps

up: ## Build and start the full stack
	$(COMPOSE) up --build -d
	@echo "consumer-web: http://localhost:5173   staff-web: http://localhost:5174"

build: ## Build images only
	$(COMPOSE) build

down: ## Stop and remove containers + volumes
	$(COMPOSE) down -v

logs: ## Tail logs from all services
	$(COMPOSE) logs -f

ps: ## Show container status
	$(COMPOSE) ps

test: ## Run Go unit tests
	go test ./...