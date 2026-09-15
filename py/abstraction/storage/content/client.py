"""Fixed content reader and writer with explicit outcomes and bounded bytes."""
import secrets

from . import rec as wire

MAX_APPEND_BYTES = 65536


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


def _cursor(value):
    return isinstance(value, str) and len(value.encode("utf-8")) <= 256


class Changes:
    """Objects a store gains or loses through one content-changes binding.

    Observe starts at the current end for an empty cursor. A gap requires
    rebuilding state from Snapshot. Calls are never retried.
    """

    def __init__(self, transport):
        self._transport = transport

    def Observe(self, cursor, max_changes, wait_ms):
        require(_cursor(cursor) and type(max_changes) is int and 1 <= max_changes <= 256
                and type(wait_ms) is int and 0 <= wait_ms <= 30000, "invalid change request")
        transport = self._transport
        if transport.deadline is None:
            import time
            transport = transport.with_waiting(deadline=time.monotonic() + transport.timeout + wait_ms / 1000,
                                               cancellation=transport.cancellation)
        page = wire.ContentChangesClient(transport).Observe(cursor, max_changes, wait_ms)
        if page.outcome != "page":
            require(not page.changes and page.next == cursor and not page.at_end, "inconsistent change refusal")
            return page
        require(len(page.changes) <= max_changes and page.next and _cursor(page.next), "inconsistent change page")
        last = 0
        for change in page.changes:
            require(type(change.sequence) is int and change.sequence > last and digest(change.digest)
                    and type(change.size) is int and change.size >= 0
                    and change.kind in ("added", "removed"), "inconsistent change entry")
            last = change.sequence
        return page

    def List(self, continuation, limit):
        require(_cursor(continuation) and type(limit) is int and 1 <= limit <= 256, "invalid listing request")
        page = wire.ContentChangesClient(self._transport).List(continuation, limit)
        if page.outcome != "page":
            require(not page.objects and not page.continuation and not page.cursor and not page.complete,
                    "inconsistent listing refusal")
            return page
        require(len(page.objects) <= limit and page.cursor and page.complete == (not page.continuation)
                and _cursor(page.continuation), "inconsistent listing page")
        previous = ""
        for listed in page.objects:
            require(digest(listed.digest) and type(listed.size) is int and listed.size >= 0
                    and listed.digest > previous, "inconsistent listed object")
            previous = listed.digest
        return page

    def Snapshot(self, limit=256):
        """Every object of one snapshot and the cursor to observe from; refusal or gap raises OutcomeError."""
        objects, continuation, cursor = [], "", ""
        while True:
            page = self.List(continuation, limit)
            if page.outcome != "page":
                raise OutcomeError(page.outcome)
            require(not cursor or page.cursor == cursor, "snapshot cursor changed between pages")
            cursor = page.cursor
            objects.extend(page.objects)
            if page.complete:
                return objects, cursor
            continuation = page.continuation


def new_request_id():
    """A caller-retained identity; keep it before Begin to reconcile a lost reply."""
    return secrets.token_hex(16)


def request_id(value):
    return (isinstance(value, str) and 16 <= len(value) <= 128
            and all(c.isascii() and (c.isalnum() or c in "_-") for c in value))


def upload(value):
    return (isinstance(value, wire.Upload) and isinstance(value.handle, str)
            and 0 < len(value.handle.encode("utf-8")) <= 128 and digest(value.digest)
            and type(value.size) is int and type(value.received) is int
            and 0 <= value.received <= value.size < 2**63)


class Writer:
    """Bounded authorized uploads through one content-writer binding."""

    def __init__(self, transport):
        self._transport = transport
        self._client = wire.ContentWriterClient(transport)

    def with_waiting(self, *, deadline=None, cancellation=None):
        return Writer(self._transport.with_waiting(
            deadline=deadline, cancellation=cancellation))

    def Begin(self, request, naming_digest, size):
        require(request_id(request) and digest(naming_digest) and type(size) is int
                and 0 <= size < 2**63, "invalid write request")
        result = self._client.Begin(request, naming_digest, size)
        stored_outcome = result.outcome in ("committed", "present")
        require((result.outcome == "started") == (result.upload is not None)
                and stored_outcome == (result.stored is not None)
                and type(result.limit) is int and result.limit >= 0,
                "inconsistent begin result")
        if result.upload is not None:
            require(upload(result.upload) and result.upload.digest == naming_digest
                    and result.upload.size == size, "inconsistent upload")
        if result.stored is not None:
            stored = result.stored
            require(stored.digest == naming_digest
                    and (result.outcome == "committed") == (stored.evidence == "hashed")
                    and (result.outcome != "committed" or stored.size == size),
                    "inconsistent stored result")
        return result

    def Append(self, value, offset, data):
        require(upload(value) and type(offset) is int and offset >= 0
                and isinstance(data, bytes) and 1 <= len(data) <= MAX_APPEND_BYTES,
                "invalid append bounds")
        result = self._client.Append(value.handle, offset, data)
        if result.outcome == "accepted":
            ok = result.received == offset + len(data) and result.received <= value.size
        elif result.outcome in ("out_of_order", "too_large"):
            ok = 0 <= result.received <= value.size
        else:
            ok = result.received == 0
        require(ok, "inconsistent append result")
        return result

    def Commit(self, value):
        require(upload(value), "invalid upload")
        result = self._client.Commit(value.handle)
        stored = result.stored
        require((result.outcome == "committed") == (stored is not None)
                and (stored is None or (stored.digest == value.digest
                                        and stored.size == value.size
                                        and stored.evidence == "hashed"))
                and (result.outcome == "incomplete" or result.received == 0)
                and 0 <= result.received <= value.size, "inconsistent commit result")
        return result

    def Abort(self, value):
        require(upload(value), "invalid upload")
        return self._client.Abort(value.handle)

    def Write(self, request, naming_digest, content):
        """Upload bytes under a caller-retained identity; one total wait budget.

        Retrying with the same identity after an uncertain failure resumes the
        live upload or returns its committed result. Write never aborts.
        """
        require(isinstance(content, bytes), "content must be bytes")
        call = Writer(self._transport.call_scope())
        begun = call.Begin(request, naming_digest, len(content))
        if begun.outcome in ("committed", "present"):
            return begun.stored
        if begun.outcome != "started":
            raise OutcomeError(begun.outcome)
        value, offset = begun.upload, begun.upload.received
        while offset < len(content):
            chunk = content[offset:offset + MAX_APPEND_BYTES]
            appended = call.Append(value, offset, chunk)
            if appended.outcome not in ("accepted", "out_of_order"):
                raise OutcomeError(appended.outcome)
            offset = appended.received
        committed = call.Commit(value)
        if committed.outcome != "committed":
            raise OutcomeError(committed.outcome)
        return committed.stored
