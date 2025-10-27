package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/paulcapestany/nostr_shared/config"
	"github.com/paulcapestany/nostr_shared/couchbase"

	"github.com/couchbase/gocb/v2"
	"github.com/gorilla/mux"
)

// nostr_threads flow
// gets an arbitrary message ID, first checks if it's already in the all-nostr-events bucket, if not, uses nak to fetch it

// Thread represents a flattened Nostr thread structure
type Thread struct {
	// NOTE: a thread's CreatedAt, ID, Kind, and Pubkey are the same as the first message in the thread (the root Nostr message)
	ID                          string                 `json:"id"`
	CreatedAt                   int64                  `json:"created_at"`
	LastMsgAt                   int64                  `json:"last_msg_at"`
	SeenAtFirst                 int64                  `json:"_seen_at_first"`
	MsgCount                    int64                  `json:"m_count"`
	Kind                        int64                  `json:"kind"`
	Pubkey                      string                 `json:"pubkey"`
	Messages                    []Message              `json:"messages"`
	XConcatenatedContent        string                 `json:"x_cat_content"`
	XLastProcessedAt            int64                  `json:"x_last_processed_at"`
	XLastProcessedTokenPosition int64                  `json:"x_last_processed_token_position"`
	XEmbeddings                 map[string]interface{} `json:"x_embeddings"`
}

// Message represents a Nostr message structure
type Message struct {
	SeenAtFirst int64         `json:"_seen_at_first"`
	CreatedAt   int64         `json:"created_at"`
	ID          string        `json:"id"`
	Content     interface{}   `json:"content"`
	Kind        int64         `json:"kind"`
	Pubkey      string        `json:"pubkey"`
	Sig         string        `json:"sig"`
	Tags        []interface{} `json:"tags"`
	// NOTE: ParentID and Depth fields are not present in the JSON payload, we have to recursively search/construct them
	ParentID     string `json:"parent_id"`
	Depth        int64  `json:"depth"`
	XTrustworthy bool   `json:"x_trustworthy"`
}

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := healthResponse{
		Status:  "ok",
		Version: serviceVersion,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("failed to write health response: %v", err)
	}
}

func serverAddr() string {
	if addr := strings.TrimSpace(os.Getenv("NOSTR_THREADS_ADDR")); addr != "" {
		return addr
	}
	return "0.0.0.0:8081"
}

func main() {
	// Initialize Couchbase connection
	config.Setup()
	cluster, _, _, _, err := couchbase.InitializeCouchbase()
	if err != nil {
		log.Fatalf("Error initializing Couchbase: %v", err)
	}
	defer cluster.Close(nil)

	addr := serverAddr()

	// Set up the HTTP server
	r := mux.NewRouter()
	r.HandleFunc("/nostr/update_thread", func(w http.ResponseWriter, r *http.Request) {
		UpdateThreadHandler(w, r, cluster)
	}).Methods("POST")
	r.HandleFunc("/healthz", healthHandler).Methods("GET")

	srv := &http.Server{
		Handler:      r,
		Addr:         addr,
		WriteTimeout: 60 * time.Second,
		ReadTimeout:  60 * time.Second,
		IdleTimeout:  90 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	done := make(chan bool)
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		<-ctx.Done()

		log.Println("Shutting down server...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		srv.SetKeepAlivesEnabled(false)
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("Could not gracefully shut down the server: %v\n", err)
		}
		close(done)
	}()

	log.Printf("Starting server on %s (version %s)", addr, serviceVersion)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Could not listen on %s: %v\n", addr, err)
	}

	<-done
	log.Println("Server stopped")
	wg.Wait()
}

func retryOperation(operation func() error, retries int, messageIDsToQuery []string) error {
	for i := 0; i < retries; i++ {
		err := operation()
		if err == nil {
			return nil
		}
		log.Printf("Retry %d/%d failed: %v messageIDsToQuery:%v", i+1, retries, err, messageIDsToQuery)
		time.Sleep(2 * time.Second) // Exponential backoff can be implemented here
	}
	return fmt.Errorf("operation failed after %d retries", retries)
}

const trustOlderTimestamps int64 = 1725897900 // Sep 9, 2024 4:05 PM as Unix timestamp

// isMessageTimestampTrustworthy checks if a message's created_at or _seen_at_first is trustworthy for appending to x_cat_content.
// Parent-child validation is always enforced, ensuring a child cannot precede its parent in terms of timestamps.
func isMessageTimestampTrustworthy(messageCreatedAt int64, lastMsgAt int64, seenAtFirst *int64, parentCreatedAt *int64, parentSeenAtFirst *int64) bool {
	// Parent-child validation: A child message cannot precede its parent in either created_at or _seen_at_first.
	if parentCreatedAt != nil && messageCreatedAt < *parentCreatedAt {
		return false
	}

	// Trust messages with a _seen_at_first value
	if seenAtFirst != nil && *seenAtFirst != 0 {
		return true
	}

	// Trust backfilled messages older than the cutoff timestamp
	if messageCreatedAt < trustOlderTimestamps {
		return true
	}

	// If no _seen_at_first, but created_at is newer than last_msg_at, trust the created_at.
	if messageCreatedAt > lastMsgAt {
		return true
	}

	// Otherwise, the timestamp is not trustworthy
	return false
}

// SanitizeContent remains unchanged
func SanitizeContent(content string) string {
	return strings.Join(strings.Fields(content), " ") // Reduce multiple spaces to a single space
}

// UpdateThreadHandler handles requests to update threads and ensure the x_cat_content is append-only
func UpdateThreadHandler(w http.ResponseWriter, r *http.Request, cluster *gocb.Cluster) {
	// Decode payload
	var payload struct {
		ID      string  `json:"id"`
		Message Message `json:"message"`
	}
	err := json.NewDecoder(r.Body).Decode(&payload)
	if err != nil {
		log.Printf("Failed to decode payload: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("Received payload with ID: %+v\n", payload.ID)

	// Couchbase setup
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bucket := cluster.Bucket(config.EnvPrefix + "threads")
	collection := bucket.DefaultCollection()

	var existingThread Thread
	var cas gocb.Cas // Store the CAS value
	// Fetch existing thread
	getResult, err := collection.Get(payload.ID, nil)
	if err == nil {
		cas = getResult.Cas() // Retrieve the CAS value
		err = getResult.Content(&existingThread)
		if err != nil {
			log.Printf("Failed to decode existing thread content: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else if errors.Is(err, gocb.ErrDocumentNotFound) {
		// Document does not exist; initialize CAS to 0
		log.Printf("Thread not found, initializing new thread")
		cas = 0
		existingThread = Thread{} // Empty thread
	} else {
		// Handle other errors
		log.Printf("Failed to get existing thread: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Process messages and fetch new ones
	messageIDsToQuery := []string{payload.ID}
	log.Println("Calling messageFetcher with messageIDsToQuery: ", messageIDsToQuery)
	var allUniqueThreadMessages []Message
	alreadyQueriedIDs := make(map[string]bool)
	foundMessageIDs := make(map[string]bool)
	err = retryOperation(func() error {
		return messageFetcher(ctx, messageIDsToQuery, &allUniqueThreadMessages, cluster, alreadyQueriedIDs, foundMessageIDs, 50)
	}, 3, messageIDsToQuery)

	if err != nil {
		log.Printf("Failed to fetch messages: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Sort messages by created_at
	sort.Slice(allUniqueThreadMessages, func(i, j int) bool {
		return allUniqueThreadMessages[i].CreatedAt < allUniqueThreadMessages[j].CreatedAt
	})

	// Process threading logic and apply parent-child validation
	threadedProcessedMessages, err := processMessageThreading(allUniqueThreadMessages)
	if err != nil {
		log.Printf("Failed to process message threading: %v messageIDsToQuery: %v", err, messageIDsToQuery)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// validate that the message is trustworthy in terms of ordering/parent-child relationships and timestamps
	for i, msg := range threadedProcessedMessages {

		// Fetch the parent message (if it exists)
		var parentCreatedAt, parentSeenAtFirst *int64
		if msg.ParentID != "" {
			for _, parentMsg := range threadedProcessedMessages {
				if parentMsg.ID == msg.ParentID {
					parentCreatedAt = &parentMsg.CreatedAt
					parentSeenAtFirst = &parentMsg.SeenAtFirst
					break
				}
			}
		}

		// Trust the message if it passes the universal trust checks and update the trust flag
		msg.XTrustworthy = isMessageTimestampTrustworthy(msg.CreatedAt, existingThread.LastMsgAt, &msg.SeenAtFirst, parentCreatedAt, parentSeenAtFirst)
		if !msg.XTrustworthy {
			log.Printf("Message %v for payload %v is not trustworthy; rejecting message.", msg.ID, payload.ID)
		}
		// Save the updated message back into the slice
		threadedProcessedMessages[i] = msg
	}

	// Update last_msg_at based on the latest trustworthy message
	var lastMsgAt int64
	for _, msg := range threadedProcessedMessages {
		if msg.XTrustworthy && msg.CreatedAt > lastMsgAt {
			lastMsgAt = msg.CreatedAt
		}
	}
	mCount := len(threadedProcessedMessages)

	// Construct the updated thread
	newThread := Thread{
		CreatedAt:   threadedProcessedMessages[0].CreatedAt,
		SeenAtFirst: threadedProcessedMessages[0].SeenAtFirst,
		LastMsgAt:   lastMsgAt,
		MsgCount:    int64(mCount),
		ID:          threadedProcessedMessages[0].ID,
		Kind:        threadedProcessedMessages[0].Kind,
		Pubkey:      threadedProcessedMessages[0].Pubkey,
		Messages:    threadedProcessedMessages,
		// XConcatenatedContent: allMessagesContent,
	}

	// Preserve existing fields
	newThread.XConcatenatedContent = existingThread.XConcatenatedContent // ???: not sure if this is correct
	newThread.XLastProcessedAt = existingThread.XLastProcessedAt
	newThread.XLastProcessedTokenPosition = existingThread.XLastProcessedTokenPosition
	newThread.XEmbeddings = existingThread.XEmbeddings

	// Pass the newThread to be merged with CAS handling
	finalThread, err := mergeThreads(existingThread, newThread)
	if err != nil {
		log.Printf("Error merging threads: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Handle the final save operation using CAS to ensure thread safety
	saveThreadWithCAS(finalThread, cas, collection)

	// Respond with the updated thread
	responseJSON, err := json.Marshal(newThread)
	if err != nil {
		log.Printf("Failed to marshal response JSON: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	w.Write(responseJSON)
}

// mergeThreads merges the existing thread with the new thread, ensuring x_cat_content is append-only
func mergeThreads(existingThread, newThread Thread) (Thread, error) {
	log.Printf("mergeThreads called, existingThread: %v newThread: %v", existingThread.ID, newThread.ID)

	if existingThread.ID == "" {
		existingThread = newThread
		// Initialize XConcatenatedContent for a new thread
		var newContent strings.Builder
		for _, msg := range newThread.Messages {
			sanitizedContent := SanitizeContent(fmt.Sprintf("%s", msg.Content))
			newContent.WriteString(sanitizedContent + "  ")
		}
		existingThread.XConcatenatedContent = newContent.String()
		return existingThread, nil
	}

	messageMap := make(map[string]Message)
	// Add all messages from existingThread to the map first
	for _, msg := range existingThread.Messages {
		messageMap[msg.ID] = msg
	}

	// Iterate over newThread messages and update/merge them
	for _, newMsg := range newThread.Messages {
		if existingMsg, exists := messageMap[newMsg.ID]; exists {
			// Merge logic: update XTrustworthy flag
			existingMsg.XTrustworthy = existingMsg.XTrustworthy || newMsg.XTrustworthy
			messageMap[newMsg.ID] = existingMsg // Update the message in the map
		} else {
			messageMap[newMsg.ID] = newMsg
		}
	}

	// Sort messages by created_at (should x_trustworthy come into play?)
	mergedMessages := make([]Message, 0, len(messageMap))
	for _, msg := range messageMap {
		mergedMessages = append(mergedMessages, msg)
	}

	sort.Slice(mergedMessages, func(i, j int) bool {
		return mergedMessages[i].CreatedAt < mergedMessages[j].CreatedAt
	})

	// Preserve existing x_cat_content and append only new content
	var allMessagesContent strings.Builder
	allMessagesContent.WriteString(existingThread.XConcatenatedContent) // Preserve existing content
	for _, msg := range mergedMessages {
		// TODO: need to refine this logic, e.g. if someone replies "lol" and it was already in x_cat_content, it doesn't get added
		// Only append content for new messages that don't already exist in x_cat_content
		if !strings.Contains(allMessagesContent.String(), SanitizeContent(fmt.Sprintf("%s", msg.Content))) {
			sanitizedContent := SanitizeContent(fmt.Sprintf("%s", msg.Content))
			allMessagesContent.WriteString(sanitizedContent + "  ")
		}

	}

	// Update last_msg_at based on the latest trustworthy message
	var lastMsgAt int64
	for _, msg := range mergedMessages {
		if msg.XTrustworthy && msg.CreatedAt > lastMsgAt {
			lastMsgAt = msg.CreatedAt
		}
	}
	mCount := len(mergedMessages)

	mergedThread := Thread{
		CreatedAt:                   existingThread.CreatedAt,
		SeenAtFirst:                 existingThread.SeenAtFirst,
		LastMsgAt:                   lastMsgAt,
		MsgCount:                    int64(mCount),
		ID:                          existingThread.ID,
		Kind:                        existingThread.Kind,
		Pubkey:                      existingThread.Pubkey,
		Messages:                    mergedMessages,
		XConcatenatedContent:        allMessagesContent.String(), // Properly append new content here
		XLastProcessedAt:            existingThread.XLastProcessedAt,
		XLastProcessedTokenPosition: existingThread.XLastProcessedTokenPosition,
		XEmbeddings:                 existingThread.XEmbeddings,
	}

	return mergedThread, nil
}

func saveThreadWithCAS(thread Thread, cas gocb.Cas, collection *gocb.Collection) {
	log.Printf("calling saveThreadWithCAS for thread: %v", thread.ID)

	const maxRetries = 5
	retries := 0
	for {
		var mutationErr error
		if cas == 0 {
			// Document does not exist, try to insert
			log.Printf("Attempting to insert new thread: %v", thread.ID)
			_, mutationErr = collection.Insert(thread.ID, thread, nil)
			if mutationErr != nil {
				if errors.Is(mutationErr, gocb.ErrDocumentExists) {
					log.Printf("Document already exists for thread: %v", thread.ID)
					// Handle the document already existing by retrieving the current CAS and thread
					getResult, err := collection.Get(thread.ID, nil)
					if err == nil {
						cas = getResult.Cas()
						var existingThread Thread
						err = getResult.Content(&existingThread)
						if err == nil {
							log.Printf("Merging threads after CAS conflict for thread: %v", thread.ID)
							thread, _ = mergeThreads(existingThread, thread)
						}
					}
				}
			} else {
				// Insert succeeded
				log.Printf("Successfully inserted thread: ID=%s, MsgCount=%d", thread.ID, thread.MsgCount)
				break
			}
		} else {
			log.Printf("Attempting to replace thread with CAS: %v", cas)
			replaceOptions := &gocb.ReplaceOptions{Cas: cas}
			_, mutationErr = collection.Replace(thread.ID, thread, replaceOptions)
			if mutationErr != nil {
				if errors.Is(mutationErr, gocb.ErrCasMismatch) {
					retries++
					log.Printf("CAS mismatch detected, retrying... (retry %d/%d)", retries, maxRetries)
					if retries >= maxRetries {
						log.Printf("Max retries reached, aborting for thread: %v", thread.ID)
						break
					}
					// Retrieve the latest CAS and thread for the mismatch
					getResult, err := collection.Get(thread.ID, nil)
					if err == nil {
						cas = getResult.Cas()
						var existingThread Thread
						err = getResult.Content(&existingThread)
						if err == nil {
							log.Printf("Merging threads after CAS mismatch for thread: %v", thread.ID)
							thread, _ = mergeThreads(existingThread, thread)
						}
					}
				}
			} else {
				// Replace succeeded
				log.Printf("Successfully replaced thread: ID=%s, MsgCount=%d", thread.ID, thread.MsgCount)
				break
			}
		}
	}
}

// messageFetcher concurrently fetches messages recursively using goroutines
func messageFetcher(ctx context.Context, messageIDs []string, allUniqueThreadMessages *[]Message, cluster *gocb.Cluster, alreadyQueriedIDs map[string]bool, foundMessageIDs map[string]bool, maxGoroutines int) error {
	if cluster == nil {
		log.Println("Cluster connection is not initialized.")
		return fmt.Errorf("cluster connection is not initialized")
	}

	var mu sync.Mutex // Mutex to protect shared resources
	messageIDsToQuery := make([]string, 0)
	messageMap := make(map[string]Message)
	for _, msg := range *allUniqueThreadMessages {
		messageMap[msg.ID] = msg
	}

	var wg sync.WaitGroup
	resultsCh := make(chan Message, len(messageIDs)) // Channel to collect results
	semaphore := make(chan struct{}, maxGoroutines)  // Semaphore to limit goroutines

	for _, id := range messageIDs {
		mu.Lock()
		if alreadyQueriedIDs[id] {
			mu.Unlock()
			continue // Skip IDs already marked as missing
		}
		mu.Unlock()

		// Acquire a spot in the semaphore
		semaphore <- struct{}{}

		// Fetch messages concurrently using goroutines
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			defer func() { <-semaphore }() // Release the semaphore when the goroutine finishes

			query := fmt.Sprintf(`WITH referencedMessages AS (
                SELECT d.*
                FROM `+"`all-nostr-events`._default._default"+` AS d
                USE KEYS "%s"

                UNION

                SELECT refMessage.*
                FROM `+"`all-nostr-events`._default._default"+` AS refMessage
                USE INDEX (kind_and_event_lookup USING GSI)
                WHERE refMessage.kind = 1 AND (ANY t IN refMessage.tags SATISFIES t[0] = "e" AND t[1] = "%s" END)
            )
            SELECT message.content, message.created_at, message._seen_at_first, message.id, message.kind, message.pubkey, message.sig, message.tags, message.x_trustworthy
            FROM referencedMessages AS message`, id, id)

			results, err := cluster.Query(query, nil)
			if err != nil {
				log.Printf("Failed to execute query for ID %s: %v", id, err)
				return
			}

			mu.Lock()
			alreadyQueriedIDs[id] = true
			mu.Unlock()

			for results.Next() {
				select {
				case <-ctx.Done():
					return
				default:
				}

				var msg Message
				if err := results.Row(&msg); err != nil {
					log.Printf("Failed to parse message: %v", err)
					continue
				}
				if msg.Kind != 1 {
					continue
				}

				contentStr := fmt.Sprintf("%v", msg.Content)
				msg.Content = contentStr

				resultsCh <- msg // Send the message to the channel
			}

			if err := results.Err(); err != nil {
				log.Printf("Error iterating results for ID %s: %v", id, err)
			}
		}(id)
	}

	go func() {
		wg.Wait()
		close(resultsCh) // Close the channel when all goroutines are done
	}()

	for msg := range resultsCh {
		mu.Lock()
		if !contains(foundMessageIDs, msg.ID) {
			messageIDsToQuery = append(messageIDsToQuery, msg.ID)
			*allUniqueThreadMessages = append(*allUniqueThreadMessages, msg)
			foundMessageIDs[msg.ID] = true
		}
		mu.Unlock()

		// Check for additional message IDs to query
		for _, tag := range msg.Tags {
			tagSlice, ok := tag.([]interface{})
			if !ok || len(tagSlice) < 2 || tagSlice[0] != "e" {
				continue
			}
			if idStr, ok := tagSlice[1].(string); ok {
				mu.Lock()
				if !contains(foundMessageIDs, idStr) && !contains(alreadyQueriedIDs, idStr) {
					messageIDsToQuery = append(messageIDsToQuery, idStr)
				}
				mu.Unlock()
			}
		}
	}

	// Recursively fetch messages for newly discovered IDs if there are any
	if len(messageIDsToQuery) > 0 {
		return messageFetcher(ctx, messageIDsToQuery, allUniqueThreadMessages, cluster, alreadyQueriedIDs, foundMessageIDs, maxGoroutines)
	}

	log.Printf("foundMessageIDs: %v", foundMessageIDs)
	return nil
}

func contains(m map[string]bool, id string) bool {
	// Safely access map values within a locked context
	return m[id]
}

func processMessageThreading(allUniqueThreadMessages []Message) ([]Message, error) {
	var messagesNestedInAThread []Message

	// Find the original message
	var originalMessage *Message
	var maxMentions int
	for i, msg := range allUniqueThreadMessages {
		etags := getETags(msg.Tags)
		if len(etags) == 0 {
			// Count the number of times the message's ID is mentioned in other messages' etags
			mentions := 0
			for _, otherMsg := range allUniqueThreadMessages {
				if otherMsg.ID != msg.ID {
					for _, etag := range getETags(otherMsg.Tags) {
						if len(etag) > 1 && etag[1] == msg.ID {
							mentions++
						}
					}
				}
			}

			if originalMessage == nil || mentions > maxMentions {
				originalMessage = &allUniqueThreadMessages[i]
				maxMentions = mentions
				// log.Printf("Warning: potential error/fail? (mentions > maxMentions) \"originalMessage.ID\": %v", originalMessage.ID)
			} else if mentions == maxMentions {
				return nil, errors.New("multiple original messages found with the same number of mentions")
			}
		}
	}

	if originalMessage == nil {
		// log.Printf("original message not found: %v", originalMessage.ID)
		return nil, errors.New("WARN: original message not found")
		// TODO: grab message from another relay?
	}

	// NOTE: this is a very naïve anti-spam measure
	// **Check if the root message contains the "invalid" tag**
	for _, tag := range originalMessage.Tags {
		tagSlice, ok := tag.([]interface{})
		if ok && len(tagSlice) > 1 && tagSlice[0] == "invalid" {
			log.Printf("Skipping thread assembly because root message contains invalid tag: %v", originalMessage.ID)
			return nil, errors.New("root message contains \"invalid\" tag")
		}
	}

	originalMessage.Depth = 1
	messagesNestedInAThread = append(messagesNestedInAThread, *originalMessage)
	allUniqueThreadMessages = removeMessage(allUniqueThreadMessages, originalMessage.ID)

	// Process direct replies to the original message
	for i := 0; i < len(allUniqueThreadMessages); i++ {
		msg := allUniqueThreadMessages[i]
		etags := getETags(msg.Tags)
		if len(etags) == 1 && etags[0][1] == originalMessage.ID {
			msg.Depth = 2
			msg.ParentID = originalMessage.ID
			messagesNestedInAThread = append(messagesNestedInAThread, msg)
			allUniqueThreadMessages = append(allUniqueThreadMessages[:i], allUniqueThreadMessages[i+1:]...)
			i--
		}
	}

	// Process remaining messages
	for len(allUniqueThreadMessages) > 0 {
		msg := allUniqueThreadMessages[0]
		etags := getETags(msg.Tags)

		var processed bool

		if len(etags) == 1 {
			// Case a: Message has only one etag
			if parentMsg := findMessageByID(messagesNestedInAThread, etags[0][1]); parentMsg != nil {
				msg.Depth = parentMsg.Depth + 1
				msg.ParentID = parentMsg.ID
				messagesNestedInAThread = append(messagesNestedInAThread, msg)
				processed = true
			}
		} else if len(etags) > 1 {
			// Case b: Message has multiple etags and one of them has etag[3] == "reply"
			for _, etag := range etags {
				if len(etag) >= 4 && etag[3] == "reply" {
					if parentMsg := findMessageByID(messagesNestedInAThread, etag[1]); parentMsg != nil {
						msg.Depth = parentMsg.Depth + 1
						msg.ParentID = parentMsg.ID
						messagesNestedInAThread = append(messagesNestedInAThread, msg)
						processed = true
						break
					}
				}
			}

			if !processed {
				// Case c: Message has multiple etags and none of them have etag[3] == "reply"
				var maxDepth int64
				var parentMsg *Message
				for _, etag := range etags {
					if msg := findMessageByID(messagesNestedInAThread, etag[1]); msg != nil && msg.Depth > maxDepth {
						maxDepth = msg.Depth
						parentMsg = msg
					}
				}
				if parentMsg != nil {
					msg.Depth = parentMsg.Depth + 1
					msg.ParentID = parentMsg.ID
					messagesNestedInAThread = append(messagesNestedInAThread, msg)
					processed = true
				}
			}
		}

		if processed {
			allUniqueThreadMessages = allUniqueThreadMessages[1:]
		} else {
			// If none of the above cases match, skip the message
			allUniqueThreadMessages = allUniqueThreadMessages[1:]
		}
	}

	return messagesNestedInAThread, nil
}

func removeMessage(messages []Message, id string) []Message {
	for i, msg := range messages {
		if msg.ID == id {
			return append(messages[:i], messages[i+1:]...)
		}
	}
	return messages
}

func getETags(tags []interface{}) [][]string {
	var etags [][]string
	for _, tag := range tags {
		if tagArr, ok := tag.([]interface{}); ok && len(tagArr) > 0 && tagArr[0] == "e" {
			var etag []string
			for _, t := range tagArr {
				if tStr, ok := t.(string); ok {
					etag = append(etag, tStr)
				}
			}
			etags = append(etags, etag)
		}
	}
	return etags
}

func findMessageByID(messages []Message, id string) *Message {
	for i := range messages {
		if messages[i].ID == id {
			return &messages[i]
		}
	}
	return nil
}
