"""Fixed content reader with explicit outcomes and bounded, unverified bytes."""
from . import rec as wire


def require(ok, message):
    if not ok:
        raise ValueError(message)


def digest(value):
    return (isinstance(value, str) and len(value) == 71
            and value.startswith("sha256:")
            and all(c in "0123456789abcdef" for c in value[7:]))


def resource(value):
    return (isinstance(value, wire.Resource) and isinstance(value.handle, str)
            and 0 < len(value.handle.encode("utf-8")) <= 128
            and digest(value.digest) and type(value.size) is int
            and 0 <= value.size < 2**63 and value.verification == "unverified")


class CopyError(RuntimeError):
    def __init__(self, confirmed, cause):
        super().__init__(str(cause))
        self.confirmed, self.cause = confirmed, cause


class OutcomeError(RuntimeError):
    def __init__(self, outcome):
        super().__init__(outcome)
        self.outcome = outcome


class Client:
    def __init__(self, transport):
        self._transport = transport
        self._client = wire.ContentReaderClient(transport)

    def with_waiting(self, *, deadline=None, cancellation=None):
        return Client(self._transport.with_waiting(
            deadline=deadline, cancellation=cancellation))

    def Open(self, naming_digest):
        require(digest(naming_digest), "canonical SHA256 digest required")
        result = self._client.Open(naming_digest)
        require((result.outcome == "opened") == (result.resource is not None),
                "inconsistent open outcome")
        if result.resource is not None:
            require(resource(result.resource) and result.resource.digest == naming_digest,
                    "inconsistent resource")
        return result

    def Read(self, value, offset, max_bytes):
        require(resource(value) and type(offset) is int and 0 <= offset <= value.size
                and type(max_bytes) is int and 1 <= max_bytes <= 65536,
                "invalid read bounds")
        result = self._client.Read(value.handle, offset, max_bytes)
        require((result.outcome == "data") == (result.chunk is not None),
                "inconsistent read outcome")
        if result.chunk is not None:
            chunk = result.chunk
            require(chunk.offset == offset and chunk.total == value.size
                    and len(chunk.data) <= max_bytes
                    and len(chunk.data) <= value.size - offset
                    and chunk.eof == (offset + len(chunk.data) == value.size)
                    and (chunk.data or chunk.eof), "inconsistent content chunk")
        return result

    def Close(self, value):
        require(resource(value), "invalid resource")
        return self._client.Close(value.handle)

    def Copy(self, value, destination):
        """One total wait budget; caller closes handle and verifies the digest.

        A partial write or non-data outcome stops immediately. confirmed counts
        bytes acknowledged by the writer; cancellation only ends this call.
        """
        confirmed = 0
        try:
            call = Client(self._transport.call_scope())
            while True:
                result = call.Read(value, confirmed, 65536)
                if result.outcome != "data":
                    raise OutcomeError(result.outcome)
                chunk = result.chunk
                if chunk.data:
                    written = destination.write(chunk.data)
                    require(type(written) is int and 0 <= written <= len(chunk.data),
                            "invalid writer count")
                    confirmed += written
                    require(written == len(chunk.data), "short write")
                if chunk.eof:
                    return confirmed
        except Exception as cause:
            raise CopyError(confirmed, cause) from cause
