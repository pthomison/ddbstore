package ddbstore

import "time"

const (
	// uuidCookieName = "uuid"

	dynamodbUuidColumnName = "uuid"
	// dynamodbSessionTableName     = "session-table"
	dynamodbExpirationColumnName = "expiration"
	dynamodbDataColumnName       = "data"

	expiryTime = 60 * time.Minute
)
