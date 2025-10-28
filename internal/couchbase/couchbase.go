package couchbase

import (
	"log"
	"time"

	"github.com/couchbase/gocb/v2"
	"github.com/paulcapestany/nostr_threads/internal/config"
)

func InitializeCouchbase() (*gocb.Cluster, *gocb.Bucket, *gocb.Collection, *gocb.Scope, error) {
	cluster, err := gocb.Connect(config.CouchbaseConnStr, gocb.ClusterOptions{
		Username: config.CouchbaseUser,
		Password: config.CouchbasePassword,
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	log.Println("Connected to Couchbase server")

	bucket := cluster.Bucket(config.EnvPrefix + "threads")
	if err := bucket.WaitUntilReady(5*time.Second, nil); err != nil {
		return nil, nil, nil, nil, err
	}

	collection := bucket.DefaultCollection()
	scope := bucket.DefaultScope()

	return cluster, bucket, collection, scope, nil
}
