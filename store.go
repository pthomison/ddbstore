package ddbstore

// Implements https://pkg.go.dev/github.com/gorilla/sessions@v1.2.2#Store
// Using DynamoDB as the underlying store
// Using https://pkg.go.dev/github.com/gorilla/sessions@v1.2.2#FilesystemStore as a ref implemenation

import (
	"context"
	"encoding/base32"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/pthomison/utilkit"
)

func NewDdbStore(region string, tableName string, keyPairs ...[]byte) (*DdbStore, error) {
	config, err := utilkit.NewConfig(utilkit.NewConfigInput{
		Region: region,
	})
	if err != nil {
		return nil, err
	}

	ddbs := &DdbStore{
		Codecs: securecookie.CodecsFromPairs(keyPairs...),
		Options: &sessions.Options{
			MaxAge: 86400 * 30,
		},
		region:    region,
		tableName: tableName,
		config:    config,
		client:    dynamodb.NewFromConfig(config),
	}

	ddbs.MaxAge(ddbs.Options.MaxAge)

	ddbs.config = config

	err = ddbs.ensureTable()
	if err != nil {
		return nil, err
	}

	return ddbs, nil
}

type DdbStore struct {
	Codecs    []securecookie.Codec
	Options   *sessions.Options // default configuration
	region    string
	tableName string
	config    aws.Config
	client    *dynamodb.Client
}

// MaxLength restricts the maximum length of new sessions to l.
// If l is 0 there is no limit to the size of a session, use with caution.
// The default for a new FilesystemStore is 4096.
func (s *DdbStore) MaxLength(l int) {
	for _, c := range s.Codecs {
		if codec, ok := c.(*securecookie.SecureCookie); ok {
			codec.MaxLength(l)
		}
	}
}

// Get returns a session for the given name after adding it to the registry.
//
// See CookieStore.Get().
func (s *DdbStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return sessions.GetRegistry(r).Get(s, name)
}

// New returns a session for the given name without adding it to the registry.
//
// See CookieStore.New().
func (s *DdbStore) New(r *http.Request, name string) (*sessions.Session, error) {
	session := sessions.NewSession(s, name)
	opts := *s.Options
	session.Options = &opts
	session.IsNew = true
	var err error
	if c, errCookie := r.Cookie(name); errCookie == nil {
		err = securecookie.DecodeMulti(name, c.Value, &session.ID, s.Codecs...)
		if err == nil {
			err = s.load(session)
			if err == nil {
				session.IsNew = false
			}
		}
	}
	return session, err
}

var base32RawStdEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// Save adds a single session to the response.
//
// If the Options.MaxAge of the session is <= 0 then the session file will be
// deleted from the store path. With this process it enforces the properly
// session cookie handling so no need to trust in the cookie management in the
// web browser.
func (s *DdbStore) Save(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	// Delete if max-age is <= 0
	if session.Options.MaxAge <= 0 {
		if err := s.erase(session); err != nil {
			return err
		}
		http.SetCookie(w, sessions.NewCookie(session.Name(), "", session.Options))
		return nil
	}

	if session.ID == "" {
		// Because the ID is used in the filename, encode it to
		// use alphanumeric characters only.
		session.ID = base32RawStdEncoding.EncodeToString(
			securecookie.GenerateRandomKey(32))
	}
	if err := s.save(session); err != nil {
		return err
	}
	encoded, err := securecookie.EncodeMulti(session.Name(), session.ID,
		s.Codecs...)
	if err != nil {
		return err
	}
	http.SetCookie(w, sessions.NewCookie(session.Name(), encoded, session.Options))
	return nil
}

// MaxAge sets the maximum age for the store and the underlying cookie
// implementation. Individual sessions can be deleted by setting Options.MaxAge
// = -1 for that session.
func (s *DdbStore) MaxAge(age int) {
	s.Options.MaxAge = age

	// Set the maxAge for each securecookie instance.
	for _, codec := range s.Codecs {
		if sc, ok := codec.(*securecookie.SecureCookie); ok {
			sc.MaxAge(age)
		}
	}
}

// DYNAMODB IMPLMENTATION

// save writes encoded session.Values to dynamodb.
func (s *DdbStore) save(session *sessions.Session) error {
	encoded, err := securecookie.EncodeMulti(session.Name(), session.Values,
		s.Codecs...)
	if err != nil {
		return err
	}

	// TODO: Check for the record existence in DDB first to not clobber the expiry
	ctx := context.TODO()
	expiration := time.Now().Add(expiryTime).Unix()

	input := &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item: map[string]types.AttributeValue{
			dynamodbUuidColumnName: &types.AttributeValueMemberS{
				Value: session.ID,
			},
			dynamodbExpirationColumnName: &types.AttributeValueMemberN{
				Value: fmt.Sprintf("%v", expiration),
			},
			dynamodbDataColumnName: &types.AttributeValueMemberS{
				Value: encoded,
			},
		},
	}

	_, err = s.client.PutItem(ctx, input)
	if err != nil {
		return err
	}

	return nil
}

// load reads a file and decodes its content into session.Values.
func (s *DdbStore) load(session *sessions.Session) error {

	ctx := context.TODO()

	output, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			dynamodbUuidColumnName: &types.AttributeValueMemberS{
				Value: session.ID,
			},
		},
	})

	if output.Item == nil {
		return nil
	}

	if err != nil {
		return err
	}

	encodedData := output.Item["data"].(*types.AttributeValueMemberS).Value

	if err = securecookie.DecodeMulti(session.Name(), string(encodedData),
		&session.Values, s.Codecs...); err != nil {
		return err
	}
	return nil
}

// delete session file
func (s *DdbStore) erase(session *sessions.Session) error {
	ctx := context.TODO()

	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			dynamodbUuidColumnName: &types.AttributeValueMemberS{
				Value: session.ID,
			},
		},
	})

	return err
}

func (ds *DdbStore) createTable() error {
	ctx := context.TODO()

	_, err := ds.client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(ds.tableName),
		AttributeDefinitions: []types.AttributeDefinition{
			{
				AttributeName: aws.String(dynamodbUuidColumnName),
				AttributeType: types.ScalarAttributeTypeS,
			},
		},
		KeySchema: []types.KeySchemaElement{{
			AttributeName: aws.String(dynamodbUuidColumnName),
			KeyType:       types.KeyTypeHash,
		}},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		return err
	}

	// Update TTL call fails directly after table creation/during provisioning
	// TODO: add a retry loop to wait for successfule Update TTL call
	time.Sleep(10 * time.Second)

	_, err = ds.client.UpdateTimeToLive(ctx, &dynamodb.UpdateTimeToLiveInput{
		TableName: aws.String(ds.tableName),
		TimeToLiveSpecification: &types.TimeToLiveSpecification{
			Enabled:       aws.Bool(true),
			AttributeName: aws.String(dynamodbExpirationColumnName),
		},
	})
	if err != nil {
		return err
	}

	return nil
}

func (ds *DdbStore) ensureTable() error {
	ctx := context.TODO()

	_, err := ds.client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(ds.tableName),
	})
	if err != nil {

		if temp := new(types.ResourceNotFoundException); errors.As(err, &temp) {
			// fmt.Printf("Resource Not Found!\n")
			return ds.createTable()
		}

		return err
	}

	return nil
}
