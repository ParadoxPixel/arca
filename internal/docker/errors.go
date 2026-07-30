package docker

// The registry API's error codes. A client reads these rather than the status
// line to tell a missing blob from a rejected manifest, so they are part of the
// protocol rather than decoration.
const (
	ErrorBlobUnknown         = "BLOB_UNKNOWN"
	ErrorBlobUploadInvalid   = "BLOB_UPLOAD_INVALID"
	ErrorBlobUploadUnknown   = "BLOB_UPLOAD_UNKNOWN"
	ErrorDigestInvalid       = "DIGEST_INVALID"
	ErrorManifestBlobUnknown = "MANIFEST_BLOB_UNKNOWN"
	ErrorManifestInvalid     = "MANIFEST_INVALID"
	ErrorManifestUnknown     = "MANIFEST_UNKNOWN"
	ErrorNameInvalid         = "NAME_INVALID"
	ErrorNameUnknown         = "NAME_UNKNOWN"
	ErrorSizeInvalid         = "SIZE_INVALID"
	ErrorTagInvalid          = "TAG_INVALID"
	ErrorUnauthorized        = "UNAUTHORIZED"
	ErrorDenied              = "DENIED"
	ErrorUnsupported         = "UNSUPPORTED"
	ErrorTooManyRequests     = "TOOMANYREQUESTS"
)

const (
	// APIVersionHeader is what a client checks to confirm it is talking to a v2
	// registry rather than to something that merely answered the ping.
	APIVersionHeader = "Docker-Distribution-API-Version"
	APIVersion       = "registry/2.0"

	ContentDigestHeader = "Docker-Content-Digest"
)
