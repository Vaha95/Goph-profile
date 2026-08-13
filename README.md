# Avatars Service

Сервис загрузки и обработки аватаров на Go. Обеспечивает загрузку изображений, хранение в S3-совместимом хранилище, асинхронное генерирование превью через RabbitMQ и REST API для управления аватарами пользователей.

## Архитектура

```
┌──────────┐       ┌───────────┐         ┌──────────┐
│  Client  │ ────> │  Server   │ ─────>  │ RabbitMQ │
└──────────┘ HTTP  │  (Echo)   │  queue  │          │
                   │  :8080    │         └────┬─────┘
                   └────┬──────┘              │
                        │                     │ consume
                  ┌─────▼──────┐         ┌────▼──────┐
                  │ PostgreSQL │         │  Worker   │
                  │            │         │ (thumbs)  │
                  └────────────┘         └────┬──────┘
                                              │
                                         ┌────▼──────┐
                                         │   MinIO   │
                                         │   (S3)    │
                                         └───────────┘
```

- **Server** (`cmd/server`) — HTTP-сервер на Echo: API, healthcheck, веб-страницы
- **Worker** (`cmd/worker`) — консьюмер очередей RabbitMQ: генерация миниатюр загруженных изображений
- **PostgreSQL** — метаданные аватаров (id, user_id, статусы, S3-ключи)
- **MinIO / S3** — хранение оригиналов и превью
- **RabbitMQ** — асинхронная очередь задач на обработку

### API endpoints

| Метод   | Путь                                         | Описание                  |
| ------- | -------------------------------------------- | ------------------------- |
| GET     | `/health`                                    | Healthcheck               |
| POST    | `/api/v1/avatars`                            | Загрузить аватар          |
| GET     | `/api/v1/avatars/:avatar_id`                 | Получить аватар           |
| DELETE  | `/api/v1/avatars/:avatar_id`                 | Удалить аватар            |
| GET     | `/api/v1/avatars/:avatar_id/metadata`        | Метаданные аватара        |
| GET     | `/api/v1/users/:user_id/avatar`              | Текущий аватар            |
| DELETE  | `/api/v1/users/:user_id/avatar`              | Удалить текущий аватар    |
| GET     | `/api/v1/users/:user_id/avatars`             | Список аватаров           |
| GET     | `/web/upload`                                | Страница загрузки         |
| GET     | `/web/gallery/:user_id`                      | Галерея пользователя      |

Модифицирующие запросы (POST/DELETE) требуют заголовок `X-User-Id`.

## Локальный запуск

### Требования

- Go 1.26
- Docker и Docker Compose

### Вариант 1 — Docker Compose (всё одной командой)

```bash
docker compose up --build
```

После первого запуска создайте бакет в MinIO:

```bash
mc alias set local http://localhost:9000 minioadmin minioadmin
mc mb local/avatars
```

Сервер будет доступен на `http://localhost:8080`.

### Вариант 2 — бинари Go + Docker для инфраструктуры

1. Поднять зависимости:

```bash
docker compose up -d postgres rabbitmq minio
```

2. Экспортировать обязательные переменные:

```bash
export S3_ACCESS_KEY=minioadmin
export S3_SECRET_KEY=minioadmin
export CORS_ALLOWED_ORIGINS="http://localhost:3000"
```

Остальные настройки подхватятся из дефолтных значений (`localhost:5432`, `localhost:9000`, `amqp://guest:guest@localhost:5672`).

3. Запустить сервер и воркер:

```bash
go run ./cmd/server &
go run ./cmd/worker &
```

Сервер автоматически применит миграции при запуске.

## Настройка

Все параметры конфигурации задаются через переменные окружения:

| Переменная             | Дефолтное значение               | Описание                     |
| ---------------------- | -------------------------------- | ---------------------------- |
| `SERVER_HOST`          | `0.0.0.0`                        | Хост HTTP-сервера            |
| `SERVER_PORT`          | `8080`                           | Порт HTTP-сервера            |
| `DB_HOST`              | `localhost`                      | Хост PostgreSQL              |
| `DB_PORT`              | `5432`                           | Порт PostgreSQL              |
| `DB_NAME`              | `avatars`                        | Имя базы данных              |
| `DB_USER`              | `postgres`                       | Пользователь БД              |
| `DB_PASSWORD`          | `postgres`                       | Пароль БД                    |
| `DB_SSL_MODE`          | `disable`                        | SSL-режим PostgreSQL         |
| `S3_ENDPOINT`          | `localhost:9000`                 | Endpoint S3/MinIO            |
| `S3_REGION`            | `us-east-1`                      | Регион S3                    |
| `S3_BUCKET`            | `avatars`                        | Имя бакета                   |
| `S3_ACCESS_KEY`        | _(required)_                     | Ключ доступа S3              |
| `S3_SECRET_KEY`        | _(required)_                     | Секретный ключ S3            |
| `S3_USE_PATH_STYLE`    | `true`                           | Path-style адреса S3         |
| `S3_SCHEME`            | `http`                           | Протокол S3                  |
| `RMQ_URL`              | `amqp://guest:guest@localhost:5672` | URL RabbitMQ              |
| `RMQ_EXCHANGE`         | `avatars.exchange`               | RabbitMQ exchange            |
| `RMQ_QUEUE`            | `avatars.processing`             | RabbitMQ очередь             |
| `MAX_UPLOAD_SIZE`      | `10485760` (10 МБ)               | Максимальный размер файла    |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:3000`          | Разрешённые CORS-оригины     |

## Тесты

```bash
go test ./...
```
