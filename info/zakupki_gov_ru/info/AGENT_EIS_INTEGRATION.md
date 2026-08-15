# Инструкция для AI-агента: сервис сбора данных ЕИС (44-ФЗ + 223-ФЗ)

Версии в этой папке:
- **44-ФЗ:** альбом ТФФ **16.2** / схемы **16.2.6.9** (итерация 1)
- **223-ФЗ:** альбом ТФФ **16.2** / схемы **16.2**

Сайт: `www.zakupki.gov.ru`.

**Домены интеграции (с ХФ 15.1.5 / 06.06.2025):**

| Домен | Когда использовать |
|---|---|
| `int.zakupki.gov.ru` | Односторонняя TLS / «браузерные» алгоритмы; альтернативная HTTPS-интеграция; getDocsIP / getDocsOrganization; часть 223 REST |
| `int44-ttls-cert.zakupki.gov.ru` | **Двусторонняя TLS (ГОСТ + клиентский сертификат)**: getDocsMis / getDocsRis / getDocsLE, очереди отдачи, «бесшовная» SOAP-интеграция |
| `int44.zakupki.gov.ru`, `int223.zakupki.gov.ru` | Устаревшие; переходный период — редирект/совместимость |

WSDL в комплекте 44-ФЗ ещё содержат `int44.zakupki.gov.ru` — в коде предпочитать домены из таблицы выше по типу auth.

---

## 0. Карта источников

| Путь | Назначение | Для сбора? |
|---|---|---|
| `44 фз/` → альбом ТФФ 16.2.6.9 (docx: «ЕИС. Альбом ТФФ_16.2» + Приложения 1–18) | Транспорт 44-ФЗ: AS2, HTTPS, SOAP, FTP-выгрузки, getDocs | **Да — главный для 44-ФЗ** |
| `44 фз/` → схемы 16.2.6.9 итерация 1 (`fcs*.xsd`, `Integration*.xsd`, `WSDL/`, `GetDocsWS/`) | XSD + **настоящие WSDL SOAP** | **Да** |
| `44 фз/` → «Интеграционный контроль и алгоритмы РК…» | ЧТЗ по реестру контрактов / выгрузкам | Вспомогательно |
| `Требования к форматам и файлам 223-ФЗ 16/` | Альбом ТФФ 223-ФЗ: HTTPS multipart, REST-выгрузки | **Да — главный для 223-ФЗ** |
| `Интеграционные схемы 223-ФЗ 16.2/` | XSD 223-ФЗ | **Да** |
| `Альбом ТФФ ГИС НР 8.1.0/` | JWT-события в ГИС НР (ЭТП↔ЕИС) | Нет для реестра закупок |
| `АТФФ ЭМ_ЕИС_v1_0_0/` | ГИС НР для электронных магазинов | Нет |

Имена подпапок в `44 фз/` на диске могут быть с битой кодировкой (после распаковки). Ориентируйся по содержимому: наличие `fcsExport.xsd` / `GetDocsWS/` / `ЕИС. … ТФФ_16.2.docx` / `Приложение N.docx`.

**Два разных транспорта:**

| Закон | Типичный транспорт сбора | Клиент |
|---|---|---|
| **44-ФЗ** | SOAP Web Services (WSDL) + региональные/rule FTP-выгрузки + getDocs* | SOAP-клиент из WSDL + HTTPS download архивов |
| **223-ФЗ** | HTTPS `multipart/form-data` REST-пути `/223/integration/...` (+ FTP-выгрузки) | HTTP multipart-клиент |

Не смешивать XSD 44 и 223 в одном парсере без явного `law=44|223`.

---

## 1. Цель агента

Сервис сбора **размещённых** сведений ЕИС:

1. забирать XML (извещения, протоколы, контракты/договоры, планы, НСИ…);
2. валидировать по соответствующим XSD;
3. нормализовать и сохранять;
4. дозабирать по реестровому номеру / периоду / региону / GUID (для 223).

Контур ГИС НР (JWT) — не цель.

---

## 2. Предпосылки доступа

Общее: регистрация у Оператора ЕИС, код ИС в «Перечне информационных систем», права в ППА на нужные `subsystemType` / организации.

| Профиль | Auth | Типичные сервисы |
|---|---|---|
| ФОИВ / MIS | Mutual TLS, сертификат УЦ ФК (ГОСТ), `sender` = код ИС | `getDocsMis` |
| РИС | Mutual TLS + права РИС | `getDocsRis`, очередь отдачи |
| Организация / ЮЛ / ФЛ после саморегистрации | По профилю getDocsOrg / getDocsLE / getDocsIP | getDocs* без полного MIS |
| ВСРЗ 223 | `login`/`password` или `systemIdentifier`+подпись | `/223/integration/rest/...` |
| КИС/РМИС 223 | `systemIdentifier` + ГОСТ-подпись строки параметров | `/223/integration/rest/kisris/...` |

Сертификаты: КЭП по 63-ФЗ, **не** ГОСТ Р 34.10-2001 (запрещён). Для mutual TLS — требования п. 2.19 альбома 44-ФЗ.

---

## 3. Архитектура

```
[scheduler]
  → collectors_44 (ftp_export | soap_getDocs | soap_queue | soap_docs_registries)
  → collectors_223 (ftp_export | rest_published | rest_nsi)
      → raw (zip/xml)
          → validate (fcsExport / IntegrationTypes / 223 XSD)
              → parse → upsert (regNumber|guid, version, law)
                  → checkpoint
```

| Компонент | Назначение |
|---|---|
| `config.law` | `44` / `223` / `both` |
| `client.soap` | WSDL из `44 фз/.../WSDL` и `GetDocsWS/**/WSDL` |
| `client.https_multipart` | 223 REST |
| `client.tls` | выбор домена `int` vs `int44-ttls-cert` |
| `collector.get_docs` | async: request → poll `getPreparedPart` → download `archiveUrl` |
| `collector.queue` | `getObject`/`getObjects` + `ack` |
| `parser.fcs` | корни `fcsNotification*`, `contract`, `contractProcedure`, … |
| `parser.223` | `purchaseNotice`, `purchaseProtocol`, `contract`, … |

---

## 4. Сбор по 44-ФЗ

### 4.1. Региональные / rule / спец-выгрузки (раздел 2.9 альбома)

XML по схеме **`fcsExport.xsd`**, в ZIP, UTF-8.  
Типы выгрузок: региональная полная, по правилам, ФАС, ОВК, НСИ, общественные обсуждения, типовые контракты, независимые гарантии, электронное актирование и др.

Часть старых описаний в альбоме помечена «устарело» — сверять актуальный статус в текущей редакции 2.9 и ЧТЗ по РК.

**Для агента:** bulk-история и ежедневная дельта публичных документов. Парсить корни из `fcsExport.xsd` (`fcsNotificationEF`, `epNotificationEF2020`, `contract`, `contractProcedure`, …).

### 4.2. SOAP «сервисы отдачи из ХД» (основной API по запросу)

Схемы: `GetDocsWS/**/getDocs*-ws-api.xsd`, описание полей — **Приложение 17**.

| Сервис | WSDL / URL path | Для кого |
|---|---|---|
| getDocsMis | `.../services-mis/getDocsMis` | ФОИВ / зарегистрированные ИС; **и 44, и 223** |
| getDocsRis | `.../services-ris/getDocsRis` | РИС |
| getDocsOrganization | `.../services/getDocsOrganization` | организации |
| getDocsLE | `.../services/getDocsLE` | ЮЛ (после саморегистрации) |
| getDocsIP | `.../services/getDocsIP` | ФЛ/ИП (после саморегистрации) |

Операции MIS (типовой сбор):

1. `getPublicDocs` / `getHiddenDocs` — по параметрам (subsystem, период, до 1000 организаций);
2. `getPublicDocsByReestrNumber` / `getHiddenDocsByReestrNumber` — по реестровому номеру;
3. `getPublicDocsByOrgRegion` / `getHiddenDocsByOrgRegion` — регион КЛАДР + тип документа (+ опция `isAllOrganizations44|223`);
4. `getNsi` — НСИ (`selectionParams44` или `selectionParams223`, `all`/`inc`);
5. `getPreparedPart` — статус подготовки архивов;
6. `getPreparedArchives` — предподготовленные архивы;
7. `getDocSignaturesByUrl` — подписи к уже полученным archiveUrl;
8. `getHiddenBankDocs`, `getAnalyticalReports`, `getDocsParticular` — спец. случаи.

**Поток:**

```
SOAP request (index.sender = код ИС)
  → response: archiveUrl[] в CDATA  ИЛИ  noData=true
  → пока архивы не готовы — getPreparedPart
  → GET archiveUrl (тот же TLS-профиль)
  → unzip → XML (fcsExport / IntegrationTypes / …)
```

Пример URL архива:  
`https://int.zakupki.gov.ru/dstore/common/download/compound?docRequestUid=...&compoundUid=...`  
(при mutual TLS часто `int44-ttls-cert...`).

Права: несовпадение `sender` и сертификата → ошибка; нет права хотя бы на одну организацию из списка → ошибка на весь запрос.

### 4.3. Очередь отдачи (раздел 2.6.2) — в основном непубличное для РИС

WSDL: `WSDL/WebServiceQueue.wsdl`, схема `queue-ws-api.xsd`.  
Методы: **`getObject`** + **`ack`** (`refId` = id полученного сообщения).  
Режим `PROD`/`TEST` в index.  
Для ВБС — отдельный `QueueVBS` (`getObjects`/`ack`).

Использовать, если нужны **непубличные** документы по подписке РИС, а не массовый публичный сбор.

### 4.4. SOAP реестров по запросу (список/карточка)

WSDL в `WSDL/`:

| WSDL | Операции | Реестр |
|---|---|---|
| `WebServiceDocsRK.wsdl` | getRKObjectList / getRKObjectInfo | Реестр контрактов |
| `WebServiceDocsRPG.wsdl` | getRPGObjectList / Info | Планы-графики |
| `WebServiceDocsKO.wsdl` | getKOObjectList / Info | Контрольные |
| `WebServiceDocsROKO.wsdl` | … | РОКО |
| `WebServiceDocsRNP.wsdl` | getRNPObjectList | РНП |
| `WebServiceDocsUnfairSupplier2022.wsdl` | … | Недобросовестные 2022 |
| `IntegrationService.wsdl` / `IncomingWebService.wsdl` | receive* / getProcessingResult | **Приём** в ЕИС (не сбор) |
| `wsEisFileStorageIncoming.wsdl` | start/chunk/finish | Файловое хранилище (загрузка) |

Схемы ответов: `docs-ws-api.xsd`, пакеты — `fcsIntegration.xsd` / `IntegrationTypes.xsd` / `IntegrationEPTypes.xsd` и др.

### 4.5. Ключевые XSD 44-ФЗ (парсинг)

| Файл | Роль |
|---|---|
| `fcsExport.xsd` | Документы **выгрузки** (то, что обычно качаем) |
| `fcsIntegration.xsd` | Интеграционные пакеты запросов/ответов |
| `fcsExtegration.xsd` | Проекты документов (часто upload-контур) |
| `IntegrationTypes.xsd` | База интеграционных типов (очень большой) |
| `IntegrationEPTypes.xsd` | Электронные процедуры / оптимизация |
| `IntegrationCPTypes.xsd` | Контракты / РЭК |
| `IntegrationKOTypes.xsd` | Контроль |
| `BaseTypes.xsd`, `CommonTypes.xsd` | Общие типы |
| `getDocsMIS-ws-api.xsd` и siblings | Запросы/ответы getDocs |
| `queue-ws-api.xsd` | Очередь |
| `what is new.txt` | Дельта версии схем |

Семантика полей — Приложения 1–18 альбома (3 — извещения, 4 — протоколы, 14 — РК/РНГ, 17 — getDocs).

### 4.6. HTTPS upload / AS2 (знать, не реализовывать для read-only)

- Альтернативная интеграция: POST `/eis-integration/services/upload`, `/uploadResult`.
- Бесшовная: SOAP `.../eis-integration/service?wsdl`.
- AS2 — для ЭП/ВСРЗ basеline; сверка пакетов `fcsSentPackageListRequest` / `fcsReceivedPackageListRequest` / `fcsReSendPackage`.

Для сервиса **только сбора** эти контуры не нужны.

---

## 5. Сбор по 223-ФЗ (кратко; детали ниже по разделам исходной инструкции)

### Каналы

1. **Региональная / rule FTP-выгрузка** (альбом 223, §2.4) — bulk. НСИ с FTP с **01.01.2025 отключены**.
2. **REST** `POST multipart` (§2.6):
   - ВСРЗ: `/223/integration/rest/.../publishedInfo/...`, `/nsi`, `integrationInfo`
   - ЭТП: `/223/integration/rest/etp/...`
   - КИС: `/223/integration/rest/kisris/...` + подпись строки параметров
3. **getDocsMis** с `selectionParams223` — тот же SOAP, что в 44-комплекте.

Версия REST в доке: **1.3**. Макс. **1000 GUID** за запрос. Ответ часто zip+base64.

### XSD 223

`Types.xsd`, `purchase.xsd`, `purchasePlan.xsd`, `contract.xsd`, `orderClause.xsd`, `webRequest.xsd`, `reference.xsd`, …

`entityType`: `purchaseNotice`, `purchaseProtocol`, `contract`, `purchasePlan`, `orderClause`, …

Подробные URL и параметры — см. разделы 5.1–5.3 ниже (сохранённый детальный разбор 223).

### 5.1. REST ВСРЗ (223)

База: `https://int.zakupki.gov.ru` (или legacy int223).

| Задача | Path | Поля |
|---|---|---|
| По GUID | `/223/integration/rest/version/publishedInfo/entityType/guid` | login, password, version=1.3, entityType, guid/guidList, startDate/endDate |
| По ЭТП | `.../publishedInfo/etp` | electronicPlaceId, период |
| План по рег.№ | `.../publishedPlan/regNumber` | regNumber, опц. all |
| По рег.№ | `/223/integration/rest/publishedInfo/regNumber` | entityType, regNumber |
| Список принятых | `/223/integration/rest/integrationInfo` | entityType, период, sortBy |
| НСИ | `/223/integration/rest/nsi` | nsiCode, nsiKind=all\|inc, requestGuid |

`nsiCode`: nsiOrganization, nsiCustomerRegistry, nsiOkpd2, nsiOkved2, nsiOkei, nsiOkv, …

### 5.2. Подпись КИС (223)

Параметры → `name=value` по алфавиту через `;` (без `signature`) → хэш ГОСТ Р 34.11-2012 → подпись ГОСТ Р 34.10-2012 → base64 в `signature`.

### 5.3. Пакет 223

`header.guid` (UUID пакета) ≠ guid сущности. `result`: success\|failure; `violations.level`: error\|warning.

---

## 6. Алгоритмы коллекторов

### 44 — getDocsMis (публичные по региону)

```
req = getPublicDocsByOrgRegionRequest(
  index.sender = SYSTEM_CODE,
  selectionParams44: orgRegion, documentType, period,
  isAllOrganizations44 = true | org list)
SOAP call (mTLS → int44-ttls-cert)
if noData: stop
else:
  poll getPreparedPart until ready
  for url in archiveUrls: download → raw → parse fcsExport → upsert
```

### 44 — очередь РИС

```
loop:
  resp = getObject()
  if noRecords: sleep; continue
  store messageBody; parse
  ack(refId = resp.id)
```

### 223 — REST период

```
requestGuid = uuid4()
POST multipart publishedInfo (entityType, startDate, endDate, auth)
decode base64 zip → validate 223 XSD → upsert by guid
```

Идемпотентность:
- 44: чаще `regNumber` + тип документа + версия/дата размещения;
- 223: `guid` сущности (+ version / registrationNumber).

---

## 7. Ошибки, лимиты, безопасность

- getDocs: полный список ошибок — Приложение 1, раздел 9 (альбом 44).
- Лимит организаций в getDocs: **1…1000**; банков: **1…100**.
- Лимит вложенных блоков ПРИЗ на приёме: **10000** (для исходящего сбора — ориентир сложности XML).
- Даты-время бизнес-полей: часовой пояс **заказчика**; формат с `Z` как «контекст заказчика» (п. 2.14 альбома 44).
- Не логировать пароли, ЭП, клиентские ключи.
- Не парсить HTML сайта вместо XML/SOAP/FTP.

---

## 8. Чего не делать

- Не строить сбор закупок на **ГИС НР** JWT.
- Не использовать один парсер без различения 44/223.
- Не хардкодить только `int44` из WSDL — выбирать домен по типу TLS.
- Не ждать НСИ 223 с FTP после 01.01.2025.
- Не реализовывать AS2/upload, если задача — только чтение.

---

## 9. MVP

**Вариант A — 44-ФЗ (рекомендуется, если есть getDocsMis + mTLS):**

1. SOAP-клиент из `WebServiceGetDocsMis.wsdl`.
2. `getPublicDocsByOrgRegion` + `getNsi` (selectionParams44).
3. Скачивание archiveUrl, парсинг `fcsNotification*` / `epNotification*` / `contract` / `contractProcedure`.
4. Checkpoint по (region, documentType, period).

**Вариант B — 223-ФЗ:**

1. REST publishedInfo + `/nsi`.
2. Парсер `purchaseNotice`, `purchaseProtocol`, `contract`/`purchaseContract`, `purchasePlan`.
3. Позже — FTP-зеркало регионов.

**Вариант C — оба закона:** общий raw-store + раздельные парсеры и конфиг `law`.

---

## 10. Чеклист

- [ ] Выбран закон и профиль доступа (MIS/РИС/ВСРЗ/…).
- [ ] Домен соответствует auth (`int` vs `int44-ttls-cert`).
- [ ] Подключены нужные XSD/WSDL из правильной папки.
- [ ] getDocs: учтён async (preparedPart) и CDATA в archiveUrl.
- [ ] 223: НСИ только через `/nsi` или getDocsNsi.
- [ ] Идемпотентный upsert + хранение raw.
- [ ] Секреты вне git.

---

## 11. Куда смотреть при сомнении

**44-ФЗ**

1. `44 фз/.../ЕИС. … ТФФ_16.2.docx` — §2.9 (выгрузки), §2.9.14–2.9.20 (getDocs + таблица URL), §2.18–2.19 (домены/сертификаты).
2. Приложение 17 — структуры getDocs.
3. Приложения 3, 4, 14 — извещения, протоколы, РК.
4. `fcsExport.xsd`, `GetDocsWS/`, `WSDL/`, `what is new.txt`.

**223-ФЗ**

1. `Требования_к_форматам_и_файлам_223_ФЗ_v_16_2.pdf` — §2.3–2.7.
2. `Интеграционные схемы 223-ФЗ 16.2/` + `webRequest.xsd`.
3. Приложения 1–3 PDF 223.

**Общее**

- ГИС НР — только события ЭТП, не реестр.
- ЧТЗ «Интеграционный контроль… РК» — нюансы выгрузки контрактов.
