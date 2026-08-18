# go-client

Go-клиент OwnLate. Порт [`@globalart/ownlate-nestjs-translator`](https://github.com/GlobalArtInc/ownlate-nestjs-translator):
переводы тянутся из OTA-бандла или из translations-map проекта, держатся в
памяти, обновляются в фоне и достаются по namespace, ключу и локали.

```bash
go get github.com/OwnLate/go-client
```

## Быстрый старт

```go
client, err := ownlate.New(ownlate.Config{
    Source: ownlate.OTASource{Bundles: []ownlate.OTABundle{{AccessKey: accessKey}}},
    Locale: "ru",
})
if err != nil {
    return err
}
defer client.Close()

client.Start(ctx) // фоновое обновление раз в 5 минут
<-client.Ready()  // первая успешная загрузка

client.T("notification.title", "en_US")
client.Translate("emails", "greeting", map[string]any{"name": "Roman"}, "ru")
```

Если фоновое обновление не нужно, вместо `Start` достаточно одного вызова
`client.Load(ctx)` — он возвращает ошибку загрузки, а `Start` её логирует и
повторяет попытку.

## Источники

**OTA** — опубликованные бандлы по access key:

```go
ownlate.OTASource{Bundles: []ownlate.OTABundle{
    {AccessKey: emailsKey, Prefix: "emails"},
    {AccessKey: pushKey, Prefix: "push"},
}}
```

Бандл без `Prefix` попадает в namespace `__ota__` (константа
`ownlate.OTANamespace`); для такого случая есть сокращение `client.T(key, locale)`.
Несколько бандлов с одним префиксом сливаются в один namespace, последний
выигрывает по совпадающим ключам.

**Map** — translations-map проекта:

```go
ownlate.MapSource{
    ProjectID: "42",
    APIKey:    apiKey,
    FilesMap:  map[string]string{"emails.json": "emails"},
}
```

Имя файла становится namespace: либо через `FilesMap`, либо само имя без
`.json`.

## Разрешение перевода

1. Локаль берётся из аргумента, иначе из `Config.Locale`.
2. Ищется namespace; для OTA-источника неизвестный namespace откатывается на
   `__ota__`.
3. Если запрошенной локали нет, берётся первая по алфавиту — так выбор
   стабилен между вызовами.
4. Неизвестный ключ возвращается как есть, поэтому пропущенный перевод не
   оставляет пустую строку.
5. Плейсхолдеры вида `{{name}}` заменяются значениями из `placeholders`.

## Настройки

| Поле `Config` | По умолчанию | Назначение |
| --- | --- | --- |
| `Source` | — | `OTASource` или `MapSource`, обязателен |
| `Locale` | пусто | локаль по умолчанию для `Translate` |
| `BaseURL` | `https://api.ownlate.com/public/v1` | адрес API |
| `PollInterval` | 5 минут | период фонового обновления |
| `RetryInterval` | 5 секунд | пауза после неудачной загрузки |
| `HTTPClient` | таймаут 30 секунд | HTTP-клиент |
| `Logger` | `slog.Default()` | куда писать ошибки загрузки |

Клиент безопасен для конкурентного использования: `Translate` читает снимок
переводов под RWMutex, обновление заменяет его целиком.
