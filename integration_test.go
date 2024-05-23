package main

import (
	"log"
	"os"
	"testing"
	"time"

	"github.com/couchbase/gocb/v2"
)

var testCluster *gocb.Cluster

func TestMain(m *testing.M) {
	var err error
	testCluster, err = setupTestCluster()
	if err != nil {
		log.Fatalf("Failed to setup test cluster: %v", err)
	}

	code := m.Run()

	// Close the test cluster connection if necessary
	if testCluster != nil {
		testCluster.Close(nil)
	}

	os.Exit(code)
}

func setupTestCluster() (*gocb.Cluster, error) {
	cluster, err := gocb.Connect("couchbase://localhost", gocb.ClusterOptions{
		Username: "admin",
		Password: "hangman8june4magician9traverse8disbar4majolica4bacilli",
	})
	if err != nil {
		return nil, err
	}
	return cluster, nil
}

func setupTestBucket(cluster *gocb.Cluster, bucketName string) (*gocb.Bucket, error) {
	bucket := cluster.Bucket(bucketName)
	return bucket, nil
}

func TestUpdateThreadHandler_NewThread(t *testing.T) {
	log.Println("Starting TestUpdateThreadHandler_NewThread")

	// Setup
	bucket, err := setupTestBucket(testCluster, "all_nostr_events")
	if err != nil {
		t.Fatalf("Failed to setup test bucket: %v", err)
	}

	message := Message{
		Content:   "This is a test message for new thread",
		CreatedAt: time.Now().Unix(),
		ID:        "test_message_new_thread",
		Kind:      1,
		Pubkey:    "pubkey_test",
		Sig:       "signature_test",
		Tags:      []interface{}{},
	}

	collection := bucket.DefaultCollection()
	_, err = collection.Upsert(message.ID, message, nil)
	if err != nil {
		t.Fatalf("Failed to insert test message into bucket: %v", err)
	}

	// Wait for the Eventing function to process the message
	time.Sleep(5 * time.Second)

	// Verify the new thread in Couchbase
	threadsBucket, err := setupTestBucket(testCluster, "threads")
	if err != nil {
		t.Fatalf("Failed to setup threads bucket: %v", err)
	}
	threadsCollection := threadsBucket.DefaultCollection()

	getResult, err := threadsCollection.Get(message.ID, nil)
	if err != nil {
		t.Fatalf("Failed to get thread from threads bucket: %v", err)
	}

	var thread Thread
	err = getResult.Content(&thread)
	if err != nil {
		t.Fatalf("Failed to decode thread content: %v", err)
	}

	if len(thread.Messages) != 1 || thread.Messages[0].ID != message.ID {
		t.Fatalf("Thread content does not match expected: %+v", thread)
	}

	// Cleanup
	cleanupTestData(bucket, []string{message.ID})
	cleanupTestData(threadsBucket, []string{message.ID})

	log.Println("TestUpdateThreadHandler_NewThread completed successfully")
}

func TestUpdateThreadHandler_ExistingThread(t *testing.T) {
	log.Println("Starting TestUpdateThreadHandler_ExistingThread")

	// Setup
	bucket, err := setupTestBucket(testCluster, "all_nostr_events")
	if err != nil {
		t.Fatalf("Failed to setup test bucket: %v", err)
	}

	parentMessage := Message{
		Content:   "This is the parent message for existing thread",
		CreatedAt: time.Now().Unix(),
		ID:        "parent_message_existing_thread",
		Kind:      1,
		Pubkey:    "pubkey_parent",
		Sig:       "signature_parent",
		Tags:      []interface{}{},
	}

	collection := bucket.DefaultCollection()
	_, err = collection.Upsert(parentMessage.ID, parentMessage, nil)
	if err != nil {
		t.Fatalf("Failed to insert parent message into bucket: %v", err)
	}

	childMessage := Message{
		Content:   "This is a child message for existing thread",
		CreatedAt: time.Now().Unix() + 1,
		ID:        "child_message_existing_thread",
		Kind:      1,
		Pubkey:    "pubkey_child",
		Sig:       "signature_child",
		Tags: []interface{}{
			[]interface{}{"e", parentMessage.ID},
		},
	}

	_, err = collection.Upsert(childMessage.ID, childMessage, nil)
	if err != nil {
		t.Fatalf("Failed to insert child message into bucket: %v", err)
	}

	// Wait for the Eventing function to process the messages
	time.Sleep(5 * time.Second)

	// Verify the updated thread in Couchbase
	threadsBucket, err := setupTestBucket(testCluster, "threads")
	if err != nil {
		t.Fatalf("Failed to setup threads bucket: %v", err)
	}
	threadsCollection := threadsBucket.DefaultCollection()

	getResult, err := threadsCollection.Get(parentMessage.ID, nil)
	if err != nil {
		t.Fatalf("Failed to get thread from threads bucket: %v", err)
	}

	var updatedThread Thread
	err = getResult.Content(&updatedThread)
	if err != nil {
		t.Fatalf("Failed to decode updated thread content: %v", err)
	}

	if len(updatedThread.Messages) != 2 || updatedThread.Messages[1].ID != childMessage.ID {
		t.Fatalf("Updated thread content does not match expected: %+v", updatedThread)
	}

	// Cleanup
	cleanupTestData(bucket, []string{parentMessage.ID, childMessage.ID})
	cleanupTestData(threadsBucket, []string{parentMessage.ID})

	log.Println("TestUpdateThreadHandler_ExistingThread completed successfully")
}

func cleanupTestData(bucket *gocb.Bucket, docIDs []string) {
	collection := bucket.DefaultCollection()
	for _, id := range docIDs {
		collection.Remove(id, nil)
	}
}
