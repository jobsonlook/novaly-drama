package server

import "testing"

func TestLocalMediaStoreTakesPrefixedImage(t *testing.T) {
	store := newLocalMediaStore()
	store.put("image-id", []byte("image-data"), "ref.jpg", "jpg")

	entry, ok := store.take(localImagePrefix + "image-id")
	if !ok {
		t.Fatal("prefixed local image was not found")
	}
	if entry.Filename != "ref.jpg" || string(entry.Data) != "image-data" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if _, ok := store.take(localImagePrefix + "image-id"); ok {
		t.Fatal("local image should be consumed once")
	}
}
