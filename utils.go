package pan_telemetry

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"encoding/base64"
	"encoding/json"

	"pan_telemetry/proto"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/ygot/ygot"
)

func DumpDeviceCapability(st *STServer, deviceReq []*proto.DeviceCapabilities) {
	for _, v := range deviceReq {
		for _, path := range v.DevicePaths {
			path, err := ygot.PathToString(path)
			if err != nil {
				st.Log.Errorf("DumpDeviceCapability: Error conv path to str; err=%v", err)
			}
			st.Log.Infof("path:%s, interval:%d\n", path, v.PublishInterval)
		}
	}
}

func LogSubscribeNotification(st *STServer, response *gnmi.SubscribeResponse, prettyprintJSON bool) error {
	fields := make(map[string]interface{})
	tags := make(map[string]string)

	switch resp := response.Response.(type) {
	case *gnmi.SubscribeResponse_Error:
		st.Log.Errorf("Subscribe notification error: code=%v, msg=%s\n",
					resp.Error.Code, resp.Error.Message)
		return errors.New(resp.Error.Message)

	case *gnmi.SubscribeResponse_Update:
		var sb strings.Builder
		sb.WriteString("{\n")
		sb.WriteString("\"timestamp\":" + strconv.FormatInt(resp.Update.Timestamp, 10) + ",\n")

		fields["timestamp"] = strconv.FormatInt(resp.Update.Timestamp, 10)

		t := time.Unix(0, resp.Update.Timestamp)
		sb.WriteString("\"time\":\"" + t.String() + "\",\n")

		fields["time"] = t.String()
		
		sb.WriteString("\"updates\": [\n    {\n")

		prefix := StrPath(resp.Update.Prefix)

		for _, update := range resp.Update.Update {
			sb.WriteString("\"Path\":\"" + path.Join(prefix, StrPath(update.Path)) + "\",\n")
			fields["Path"] = path.Join(prefix, StrPath(update.Path))
			sb.WriteString("\"values\":" + StrUpdateVal(update, prettyprintJSON) + "\n")
			fields["values"] = StrUpdateVal(update, prettyprintJSON)
			st.acc.AddFields("", fields, tags)
		}

		sb.WriteString("    }\n  ]\n}")
		st.Log.Infof("%s",sb.String())

	default:
		st.Log.Error("LogSubscribeNotification: Unexpected response notification type")
		return errors.New("Unexpected response notification type")
	}

	return nil
}

func StrUpdateVal(update *gnmi.Update, prettyprintJSON bool) string {
	if update.Value != nil {
		switch update.Value.Type {
		case gnmi.Encoding_JSON, gnmi.Encoding_JSON_IETF:
			return StrJSON(update.Value.Value, prettyprintJSON)
		case gnmi.Encoding_BYTES, gnmi.Encoding_PROTO:
			return base64.StdEncoding.EncodeToString(update.Value.Value)
		case gnmi.Encoding_ASCII:
			return string(update.Value.Value)
		default:
			return string(update.Value.Value)
		}
	}
	return StrVal(update.Val, prettyprintJSON)
}

func StrJSON(inJSON []byte, prettyprintJSON bool) string {
	var (
		out bytes.Buffer
		err error
	)
	// Check for ',' as simple heuristic on whether to expand JSON
	// onto multiple lines, or compact it to a single line.
	if prettyprintJSON && bytes.Contains(inJSON, []byte{','}) {
		err = json.Indent(&out, inJSON, "", "  ")
	} else {
		err = json.Compact(&out, inJSON)
	}
	if err != nil {
		return fmt.Sprintf("(error unmarshalling json: %s)\n", err) + string(inJSON)
	}
	return out.String()
}

func StrVal(val *gnmi.TypedValue, prettyprintJSON bool) string {
	switch v := val.GetValue().(type) {
	case *gnmi.TypedValue_StringVal:
		return v.StringVal
	case *gnmi.TypedValue_JsonIetfVal:
		return StrJSON(v.JsonIetfVal, prettyprintJSON)
	case *gnmi.TypedValue_JsonVal:
		return StrJSON(v.JsonVal, prettyprintJSON)
	case *gnmi.TypedValue_IntVal:
		return strconv.FormatInt(v.IntVal, 10)
	case *gnmi.TypedValue_UintVal:
		return strconv.FormatUint(v.UintVal, 10)
	case *gnmi.TypedValue_BoolVal:
		return strconv.FormatBool(v.BoolVal)
	case *gnmi.TypedValue_BytesVal:
		return base64.StdEncoding.EncodeToString(v.BytesVal)
	case *gnmi.TypedValue_DecimalVal:
		return StrDecimal64(v.DecimalVal)
	case *gnmi.TypedValue_FloatVal:
		return strconv.FormatFloat(float64(v.FloatVal), 'g', -1, 32)
	case *gnmi.TypedValue_DoubleVal:
		return strconv.FormatFloat(float64(v.DoubleVal), 'g', -1, 64)
	case *gnmi.TypedValue_LeaflistVal:
		return StrLeaflist(v.LeaflistVal)
	case *gnmi.TypedValue_AsciiVal:
		return v.AsciiVal
	case *gnmi.TypedValue_AnyVal:
		return v.AnyVal.String()
	case *gnmi.TypedValue_ProtoBytes:
		return base64.StdEncoding.EncodeToString(v.ProtoBytes)
	case nil:
		return ""
	default:
		panic(v)
	}
	return ""
}

// strLeafList builds a human-readable form of a leaf-list. e.g. [1, 2, 3] or [a, b, c]
func StrLeaflist(v *gnmi.ScalarArray) string {
	var b strings.Builder
	b.WriteByte('[')

	for i, elm := range v.Element {
		b.WriteString(StrVal(elm, false))
		if i < len(v.Element)-1 {
			b.WriteString(", ")
		}
	}

	b.WriteByte(']')
	return b.String()
}

func StrDecimal64(d *gnmi.Decimal64) string {
	var i, frac int64
	if d.Precision > 0 {
		div := int64(10)
		it := d.Precision - 1
		for it > 0 {
			div *= 10
			it--
		}
		i = d.Digits / div
		frac = d.Digits % div
	} else {
		i = d.Digits
	}
	format := "%d.%0*d"
	if frac < 0 {
		if i == 0 {
			// The integer part doesn't provide the necessary minus sign.
			format = "-" + format
		}
		frac = -frac
	}
	return fmt.Sprintf(format, i, int(d.Precision), frac)
}

// StrPath builds a human-readable form of a gnmi path.
// e.g. /a/b/c[e=f]
func StrPath(path *gnmi.Path) string {
	if path == nil {
		return "/"
	} else if len(path.Elem) != 0 {
		return StrPathV04(path)
	} else if len(path.Element) != 0 {
		return StrPathV03(path)
	}
	return "/"
}

// StrPathV03 handles the v0.3 gnmi and earlier path.Element member.
func StrPathV03(path *gnmi.Path) string {
	return "/" + strings.Join(path.Element, "/")
}

// strPathV04 handles the v0.4 gnmi and later path.Elem member.
func StrPathV04(path *gnmi.Path) string {
	b := &strings.Builder{}
	for _, elm := range path.Elem {
		b.WriteRune('/')
		WriteElem(b, elm)
	}
	return b.String()
}

func WriteElem(b *strings.Builder, elm *gnmi.PathElem) {
	b.WriteString(EscapeName(elm.Name))
	if len(elm.Key) > 0 {
		WriteKey(b, elm.Key)
	}
}

// writeKey is used as a helper to contain the logic of writing keys as a string.
func WriteKey(b *strings.Builder, key map[string]string) {
	// Sort the keys so that they print in a consistent
	// order. We don't have the YANG AST information, so the
	// best we can do is sort them alphabetically.
	size := 0
	keys := make([]string, 0, len(key))
	for k, v := range key {
		keys = append(keys, k)
		size += len(k) + len(v) + 3 // [, =, ]
	}
	sort.Strings(keys)
	b.Grow(size)
	for _, k := range keys {
		b.WriteByte('[')
		b.WriteString(EscapeKey(k))
		b.WriteByte('=')
		b.WriteString(EscapeValue(key[k]))
		b.WriteByte(']')
	}
}

func EscapeKey(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `=`, `\=`)
	return s
}

func EscapeValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `]`, `\]`)
	return s
}

func EscapeName(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `/`, `\/`)
	s = strings.ReplaceAll(s, `[`, `\[`)
	return s
}
