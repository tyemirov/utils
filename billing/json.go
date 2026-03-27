package billing

import "encoding/json"

// jsonMarshalFunc is the JSON marshal function used by the billing package.
// It defaults to json.Marshal and can be replaced for dependency injection.
var jsonMarshalFunc = json.Marshal
