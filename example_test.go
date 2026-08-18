package ownlate_test

import (
	"context"
	"fmt"

	ownlate "github.com/OwnLate/go-client"
)

func ExampleClient() {
	client, err := ownlate.New(ownlate.Config{
		Source: ownlate.OTASource{Bundles: []ownlate.OTABundle{
			{AccessKey: "06a1beee8c794ff99443bb9da888bc84ccc5c4b000a1427da6f9f9f17569f12e"},
		}},
		Locale: "ru",
	})
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// Keep the translations fresh in the background and wait for the first
	// load before serving traffic.
	ctx := context.Background()
	client.Start(ctx)
	<-client.Ready()

	fmt.Println(client.T("notification.title", "en_US"))
	fmt.Println(client.Translate("emails", "greeting", map[string]any{"name": "Roman"}, "ru"))
}

func ExampleClient_mapSource() {
	client, err := ownlate.New(ownlate.Config{
		Source: ownlate.MapSource{
			ProjectID: "42",
			APIKey:    "ownlate-api-key",
			FilesMap:  map[string]string{"emails.json": "emails"},
		},
		Locale: "ru",
	})
	if err != nil {
		panic(err)
	}
	defer client.Close()

	if err := client.Load(context.Background()); err != nil {
		panic(err)
	}

	fmt.Println(client.Translate("emails", "subject", nil, ""))
}
