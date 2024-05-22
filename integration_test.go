package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
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

func setupTestBucket(cluster *gocb.Cluster) (*gocb.Bucket, error) {
	bucket := cluster.Bucket("all_nostr_events")
	return bucket, nil
}

func cleanupTestData(bucket *gocb.Bucket, docIDs []string) {
	collection := bucket.DefaultCollection()
	for _, id := range docIDs {
		collection.Remove(id, nil)
	}
}

func TestUpdateThreadHandler_NewThread(t *testing.T) {
	// Setup
	bucket, err := setupTestBucket(testCluster)
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

	payload := struct {
		ID      string  `json:"id"`
		Message Message `json:"message"`
	}{
		ID:      message.ID,
		Message: message,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	req, err := http.NewRequest("POST", "/nostr/update_thread", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		UpdateThreadHandler(w, r, testCluster)
	})

	// Test
	handler.ServeHTTP(rr, req)

	// Verify
	if status := rr.Code; status != http.StatusOK {
		t.Fatalf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	// Verify the new thread in Couchbase
	collection := bucket.DefaultCollection()
	getResult, err := collection.Get(message.ID, nil)
	if err != nil {
		t.Fatalf("Failed to get thread from bucket: %v", err)
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
}

func TestUpdateThreadHandler_ExistingThread(t *testing.T) {
	// Setup
	bucket, err := setupTestBucket(testCluster)
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

	thread := Thread{
		CreatedAt: parentMessage.CreatedAt,
		ID:        parentMessage.ID,
		Kind:      parentMessage.Kind,
		Pubkey:    parentMessage.Pubkey,
		Messages:  []Message{parentMessage},
	}

	collection := bucket.DefaultCollection()
	_, err = collection.Upsert(parentMessage.ID, thread, nil)
	if err != nil {
		t.Fatalf("Failed to insert parent thread into bucket: %v", err)
	}

	childMessage := Message{
		Content:   "This is a child message for existing thread",
		CreatedAt: time.Now().Unix() + 1,
		ID:        "child_message_existing_thread",
		Kind:      1,
		Pubkey:    "pubkey_child",
		Sig:       "signature_child",
		Tags:      []interface{}{},
		ParentID:  parentMessage.ID,
	}

	payload := struct {
		ID      string  `json:"id"`
		Message Message `json:"message"`
	}{
		ID:      childMessage.ID,
		Message: childMessage,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	req, err := http.NewRequest("POST", "/nostr/update_thread", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		UpdateThreadHandler(w, r, testCluster)
	})

	// Test
	handler.ServeHTTP(rr, req)

	// Verify
	if status := rr.Code; status != http.StatusOK {
		t.Fatalf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	// Verify the updated thread in Couchbase
	getResult, err := collection.Get(parentMessage.ID, nil)
	if err != nil {
		t.Fatalf("Failed to get parent thread from bucket: %v", err)
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
}
