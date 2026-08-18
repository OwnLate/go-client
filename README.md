# go-client

The Go client for OwnLate. A port of
[`@globalart/ownlate-nestjs-translator`](https://github.com/GlobalArtInc/ownlate-nestjs-translator):
translations are pulled from an OTA bundle or from the translations map of a
project, kept in memory, refreshed in the background and resolved by namespace,
key and locale.

```bash
go get github.com/OwnLate/go-client
```

## Quick start

```go
client, err := ownlate.New(ownlate.Config{
    Source: ownlate.OTASource{Bundles: []ownlate.OTABundle{{AccessKey: accessKey}}},
    Locale: "ru",
})
if err != nil {
    return err
}
defer client.Close()

client.Start(ctx) // refresh in the background every five minutes
<-client.Ready()  // the first successful load

client.T("notification.title", "en_US")
client.Translate("emails", "greeting", map[string]any{"name": "Roman"}, "ru")
```

If the background refresh is not needed, a single `client.Load(ctx)` replaces
`Start`: it returns the load error, whereas `Start` logs it and retries.

## Sources

**OTA** — published bundles addressed by access key:

```go
ownlate.OTASource{Bundles: []ownlate.OTABundle{
    {AccessKey: emailsKey, Prefix: "emails"},
    {AccessKey: pushKey, Prefix: "push"},
}}
```

A bundle without a `Prefix` lands in the `__ota__` namespace (the
`ownlate.OTANamespace` constant); `client.T(key, locale)` is the shorthand for
that case. Several bundles sharing a prefix are merged into one namespace, and
the last one wins on colliding keys.

**Map** — the translations map of a project:

```go
ownlate.MapSource{
    ProjectID: "42",
    APIKey:    apiKey,
    FilesMap:  map[string]string{"emails.json": "emails"},
}
```

The file name becomes the namespace: either through `FilesMap` or as the name
itself without the `.json` suffix.

## How a translation is resolved

1. The locale comes from the argument, otherwise from `Config.Locale`.
2. The namespace is looked up; for an OTA source an unknown namespace falls back
   to `__ota__`.
3. If the requested locale is missing, the first one in alphabetical order is
   used, which keeps the choice stable between calls.
4. An unknown key is returned as is, so a missing translation never leaves an
   empty string behind.
5. Placeholders written as `{{name}}` are replaced with the values from
   `placeholders`.

## Configuration

| `Config` field | Default | Purpose |
| --- | --- | --- |
| `Source` | — | `OTASource` or `MapSource`, required |
| `Locale` | empty | the locale `Translate` uses when the call site passes none |
| `BaseURL` | `https://api.ownlate.com/public/v1` | API address |
| `PollInterval` | 5 minutes | background refresh period |
| `RetryInterval` | 5 seconds | pause after a failed load |
| `HTTPClient` | 30 second timeout | HTTP client |
| `Logger` | `slog.Default()` | where load failures are reported |

The client is safe for concurrent use: `Translate` reads a snapshot under an
RWMutex and a refresh replaces that snapshot as a whole.
