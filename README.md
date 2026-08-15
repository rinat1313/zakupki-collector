# zakupki-collector

Сервис сбора закупок ЕИС (`zakupki.gov.ru`) и API поверх PostgreSQL.

Документация интеграции: `info/zakupki_gov_ru/info/AGENT_EIS_INTEGRATION.md`.

## Архитектура

| Сервис | Назначение |
|---|---|
| **collector** | Каждые **29 минут** запрашивает ЕИС (SOAP getDocs) за окно **30 минут**, парсит извещения и делает upsert в PostgreSQL |
| **api** | CRUD + поиск по закупкам в PostgreSQL |
| **internal/store** | Модуль работы с БД (создание/чтение/изменение/удаление + поиск) |

Идемпотентность: при повторной загрузке запись **не удаляется**. Если `last_updated_at` не изменился — запись **пропускается**; иначе обновляется.

## Поля тендера

- номер закупки (`purchase_number`)
- описание (`description`)
- заказчик (`customer`)
- ИНН заказчика (`customer_inn`)
- НМЦ (`nmck`)
- дата завершения (`end_date`)
- дата последнего обновления (`last_updated_at`)

## Быстрый старт

```bash
cp .env.example .env
# укажите EIS_TOKEN (UUID пользователя / код ИС)
docker compose up -d postgres
go run ./cmd/api
# в другом терминале:
go run ./cmd/collector
```

Или всё через Docker:

```bash
export EIS_TOKEN='xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx'
docker compose up --build
```

Для mutual TLS положите сертификаты в `./certs` и задайте `EIS_TLS_*` / endpoint `int44-ttls-cert.zakupki.gov.ru` (см. `.env.example`).

## API

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/healthz` | healthcheck |
| `GET` | `/tenders/{number}` | строгое соответствие номеру закупки |
| `GET` | `/tenders?number_like=` | приближённый поиск по номеру |
| `GET` | `/tenders?q=слово1,слово2&exclude=слово3` | описание содержит слова, без исключений |
| `GET` | `/tenders/search?include=...&exclude=...` | то же явно |
| `POST` | `/tenders` | создать |
| `PUT` | `/tenders/{number}` | изменить |
| `DELETE` | `/tenders/{number}` | удалить |

Пример поиска «содержит сервер, но не мебель»:

```bash
curl 'http://localhost:8080/tenders/search?include=сервер&exclude=мебель'
```

## Конфиг collector

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `EIS_TOKEN` | — | UUID / токен пользователя (`index.sender`) |
| `EIS_LOOKBACK` | `30m` | окно «последних» тендеров |
| `EIS_INTERVAL` | `29m` | период опроса |
| `EIS_ORG_REGION` | `77` | регион КЛАДР |
| `EIS_SUBSYSTEM` | `PRIZ` | подсистема извещений |
| `EIS_DOC_TYPES` | ep/fcs notification… | типы документов getDocs |

Сбор идёт через `getPublicDocsByOrgRegion` + при необходимости `getPreparedPart` и скачивание `archiveUrl` (альбом ТФФ 44-ФЗ, getDocsMis).

## Разработка

```bash
go test ./internal/eis/...
DATABASE_URL='postgres://zakupki:zakupki@localhost:5432/zakupki?sslmode=disable' go test ./...
```
