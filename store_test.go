package ddbstore

import (
	"net/http/httptest"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/gorilla/securecookie"
	"github.com/pthomison/utilkit"
)

func TestHelloWorld(t *testing.T) {
	store, err := NewDdbStore("us-east-2", "store-test-table", securecookie.GenerateRandomKey(32))
	utilkit.CheckTest(err, t)

	request := httptest.NewRequest("GET", "/", nil)
	responseRecorder := httptest.NewRecorder()

	sesh, err := store.New(request, "test-session")
	utilkit.CheckTest(err, t)

	sesh.Values["hello"] = "world"

	err = sesh.Save(request, responseRecorder)
	utilkit.CheckTest(err, t)

	spew.Dump(responseRecorder.Header())

	secondRequest := httptest.NewRequest("GET", "/", nil)
	secondRequest.Header.Add("Cookie", responseRecorder.Result().Header.Get("Set-Cookie"))

	sesh, err = store.New(secondRequest, "test-session")
	utilkit.CheckTest(err, t)

	spew.Dump(sesh.Values)

}

// type testingHarness struct {
// 	t *testing.T
// 	k *Key
// }

// func TestFreshDynamoDbSession(t *testing.T) {
// 	harness := testingHarness{
// 		t: t,
// 		k: TestingKey,
// 	}

// 	data := map[interface{}]interface{}{
// 		"string":  gofakeit.Sentence(10),
// 		"number":  rand.Int(),
// 		"float32": rand.Float32(),
// 		"float64": rand.Float64(),
// 		"bool":    rand.Int()%2 == 0,
// 		"large":   gofakeit.Sentence(1000),
// 	}

// 	h := http.Header{}
// 	h.Set("Cookie", uuidCookie("").String())
// 	r := &http.Request{
// 		Header: h,
// 	}

// 	sesh, err := NewSession(harness.k, r)
// 	usefulgo.CheckTest(err, harness.t)

// 	err = sesh.Write(r, httptest.NewRecorder(), &SessionState{
// 		DynamoDbState: data,
// 	})
// 	usefulgo.CheckTest(err, harness.t)

// 	state, err := sesh.Read(r)
// 	usefulgo.CheckTest(err, harness.t)

// 	for k, v := range data {
// 		dv, ok := state.DynamoDbState[k]
// 		if !ok {
// 			t.Error("Key not found in the dynamo data: ", k)
// 		} else {
// 			if dv != v {
// 				t.Error("Value Error in the dynamo data: ", k, v, dv)
// 			} else {
// 				logrus.Infof("Successful storage & retrieval of %v\n", v)
// 			}
// 		}
// 	}

// }
