include .env
export

export PROJECT_ROOT=${shell pwd}

env-up:
	@docker compose up -d golang-kp-postgres golang-kp-s3

env-down:
	@docker compose down golang-kp-postgres golang-kp-s3

env-cleanup:
	@read -p "Очистить все volume файлы окружения? Опасность утери данных. [Y/N]: " ans; \
	if [ "$$ans" = "Y" ]; then \
	  make env-down  && \
	  make env-port-close && \
	  rm -rf ${PROJECT_ROOT}/out/pgdata && \
	  rm -rf ${PROJECT_ROOT}/out/s3data && \
	  echo "Файлы окружения успешно очищены"; \
	else \
	  echo "Очистка окружения отменена"; \
	fi

env-port-forwarder:
	@docker compose up -d port-forwarder

env-port-close:
	@docker compose down port-forwarder

migrate-create:
	@if [ -z "$(seq)" ]; then \
	    echo "Отсутствует обязательный параметр seq. Пример использования make migrate-create seq=init"; \
	    exit 1; \
	fi; \
	docker compose run --rm golang-kp-migrate \
		create \
		-ext sql \
		-dir /migrations \
		-seq "$(seq)"

migrate-up:
	@make migrate-action action=up n=$(n)

migrate-down:
	@make migrate-action action=down n=$(if $(n),$(n),1)

migrate-ensure-schema:
	@docker compose exec -T golang-kp-postgres \
		psql -U ${POSTGRES_USER} -d ${POSTGRES_DB} -v ON_ERROR_STOP=1 \
		-c "CREATE SCHEMA IF NOT EXISTS golang_kp;" > /dev/null

migrate-action:
	@if [ -z "$(action)" ]; then \
	    echo "Отсутствует обязательный параметр action."; \
		exit 1; \
	fi; \
	make migrate-ensure-schema && \
	docker compose run --rm golang-kp-migrate \
		-path /migrations \
		-database "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@golang-kp-postgres:5432/${POSTGRES_DB}?sslmode=disable&search_path=golang_kp" \
		$(action) $(n)

app-run:
	@$(MAKE) run-movies & \
	$(MAKE) run-actors & \
	wait

run-movies:
	@export LOGGER_FOLDER=${PROJECT_ROOT}/out/logs/movies && \
	export POSTGRES_HOST=localhost && \
	export HTTP_ADDRESS=:5055 && \
	go run ${PROJECT_ROOT}/cmd/movies-service

run-actors:
	@export LOGGER_FOLDER=${PROJECT_ROOT}/out/logs/actors && \
	export POSTGRES_HOST=localhost && \
	export HTTP_ADDRESS=:5056 && \
	go run ${PROJECT_ROOT}/cmd/actors-service

kafka-create-topics:
	@docker compose exec -T golang-kp-kafka /opt/kafka/bin/kafka-topics.sh \
		--bootstrap-server localhost:9092 \
		--create --if-not-exists \
		--topic actors.events \
		--partitions 3 \
		--replication-factor 1

kafka-tail:
	@docker compose exec golang-kp-kafka /opt/kafka/bin/kafka-console-consumer.sh \
		--bootstrap-server localhost:9092 \
		--topic $(topic) \
		--from-beginning \
		--property print.key=true

proto-gen:
	@buf lint && buf generate