package store

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	databaseName   = "frmusic"
	guildCollName  = "guild_playback"
)

// Track is a public-facing track snapshot for the website/API.
type Track struct {
	Title       string `bson:"title" json:"title"`
	Artist      string `bson:"artist" json:"artist"`
	Thumbnail   string `bson:"thumbnail,omitempty" json:"thumbnail,omitempty"`
	PageURL     string `bson:"page_url,omitempty" json:"page_url,omitempty"`
	DurationSec int    `bson:"duration_sec,omitempty" json:"duration_sec,omitempty"`
	Requester   string `bson:"requester,omitempty" json:"requester,omitempty"`
}

// GuildPlayback is the live player state for one Discord server.
type GuildPlayback struct {
	GuildID    string    `bson:"_id" json:"guild_id"`
	GuildName  string    `bson:"guild_name,omitempty" json:"guild_name,omitempty"`
	Now        *Track    `bson:"now,omitempty" json:"now,omitempty"`
	Upcoming   []Track   `bson:"upcoming" json:"upcoming"`
	Paused     bool      `bson:"paused" json:"paused"`
	UpcomingN  int       `bson:"upcoming_count" json:"upcoming_count"`
	UpdatedAt  time.Time `bson:"updated_at" json:"updated_at"`
}

// Store persists per-guild playback snapshots.
type Store struct {
	client *mongo.Client
	coll   *mongo.Collection
}

// Connect opens MongoDB using the Atlas URI.
func Connect(ctx context.Context, uri string) (*Store, error) {
	uri = trim(uri)
	if uri == "" {
		return nil, fmt.Errorf("mongodb uri is empty")
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("connecting to mongodb: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("pinging mongodb: %w", err)
	}

	return &Store{
		client: client,
		coll:   client.Database(databaseName).Collection(guildCollName),
	}, nil
}

// Close disconnects the client.
func (s *Store) Close(ctx context.Context) error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Disconnect(ctx)
}

// UpsertGuildPlayback writes the latest player snapshot for a guild.
func (s *Store) UpsertGuildPlayback(ctx context.Context, doc GuildPlayback) error {
	if s == nil {
		return nil
	}
	if doc.GuildID == "" {
		return fmt.Errorf("guild id is required")
	}
	if doc.Upcoming == nil {
		doc.Upcoming = []Track{}
	}
	doc.UpcomingN = len(doc.Upcoming)
	doc.UpdatedAt = time.Now().UTC()

	opts := options.Update().SetUpsert(true)
	_, err := s.coll.UpdateByID(ctx, doc.GuildID, bson.M{"$set": doc}, opts)
	if err != nil {
		return fmt.Errorf("upserting guild playback: %w", err)
	}
	return nil
}

// GetGuildPlayback returns one guild snapshot.
func (s *Store) GetGuildPlayback(ctx context.Context, guildID string) (*GuildPlayback, error) {
	if s == nil {
		return nil, fmt.Errorf("store is not configured")
	}
	var doc GuildPlayback
	err := s.coll.FindOne(ctx, bson.M{"_id": guildID}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return &GuildPlayback{GuildID: guildID, Upcoming: []Track{}}, nil
	}
	if err != nil {
		return nil, err
	}
	if doc.Upcoming == nil {
		doc.Upcoming = []Track{}
	}
	return &doc, nil
}

// ListRecent returns recent guild snapshots for the website directory.
func (s *Store) ListRecent(ctx context.Context, limit int64) ([]GuildPlayback, error) {
	if s == nil {
		return nil, fmt.Errorf("store is not configured")
	}
	if limit <= 0 {
		limit = 20
	}
	opts := options.Find().SetSort(bson.D{{Key: "updated_at", Value: -1}}).SetLimit(limit)
	cur, err := s.coll.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var out []GuildPlayback
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []GuildPlayback{}
	}
	return out, nil
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
