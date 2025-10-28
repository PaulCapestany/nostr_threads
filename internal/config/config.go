package config

import (
	"fmt"
	"log"
	"os"
)

var CouchbaseUser string
var CouchbasePassword string
var CouchbaseConnStr string
var EnvPrefix string

func Setup() {
	CouchbaseUser = os.Getenv("COUCHBASE_USER")
	CouchbasePassword = os.Getenv("COUCHBASE_PASSWORD")
	CouchbaseConnStr = resolveCouchbaseConnString()
	EnvPrefix = os.Getenv("ENV_PREFIX")
	if EnvPrefix != "" {
		EnvPrefix += "-"
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	if commitSHA := os.Getenv("GIT_COMMIT_HASH"); commitSHA != "" {
		log.SetPrefix("[" + commitSHA + "] ")
	}

	validateEnvVars()
}

func resolveCouchbaseConnString() string {
	if conn := os.Getenv("COUCHBASE_CONNSTR"); conn != "" {
		return conn
	}

	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}

	return fmt.Sprintf("couchbase://couchbase-cluster.%s.svc.cluster.local", namespace)
}

func validateEnvVars() {
	for _, key := range []string{"COUCHBASE_USER", "COUCHBASE_PASSWORD"} {
		if os.Getenv(key) == "" {
			log.Fatalf("FATAL: Environment variable %s is not set", key)
		}
	}
}
