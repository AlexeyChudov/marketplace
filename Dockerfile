# Многоступенчатая сборка: компилируем в одном образе, запускаем в другом.
# Итоговый образ — только статический бинарь, без Go-тулчейна.

FROM golang:1.26-alpine AS build
WORKDIR /src

# Сначала только манифесты — этот слой кэшируется, пока зависимости не менялись.
COPY go.mod go.su[m] ./
RUN go mod download

COPY . .
# CGO_ENABLED=0 даёт статический бинарь, которому не нужен libc в рантайме.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/marketplace-api .

FROM alpine:3.22
RUN adduser -D -u 10001 app
COPY --from=build /out/marketplace-api /usr/local/bin/marketplace-api
USER app

# Платформа сама передаст PORT и сама пробросит его наружу.
# Флаг --addr не указываем — сервис возьмёт порт из окружения.
ENTRYPOINT ["/usr/local/bin/marketplace-api"]
