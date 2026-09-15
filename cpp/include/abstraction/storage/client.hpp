#pragma once
#include <abstraction/storage/content/rec.h>
#include <abstraction/ipc/frame.hpp>
#include <algorithm>
#include <cstdint>
#include <optional>
#include <random>
#include <stdexcept>
#include <string>
#include <vector>
namespace abstraction::storage {
namespace content_detail {
inline bool digest(const std::string& d){return d.size()==71&&d.compare(0,7,"sha256:")==0&&std::all_of(d.begin()+7,d.end(),[](unsigned char c){return(c>='0'&&c<='9')||(c>='a'&&c<='f');});}
inline bool resource(const content::Resource& r){return !r.handle.empty()&&r.handle.size()<=128&&r.size>=0&&r.verification=="unverified"&&digest(r.digest);}
inline void read(const content::ReadResult& result,const content::Resource& r,std::int64_t offset,std::int64_t max_bytes){
 if((result.outcome=="data")!=result.chunk.has_value())throw content::ServiceError("invalid_storage","inconsistent read outcome");
 if(!result.chunk)return;const auto& c=*result.chunk;
 if(c.offset!=offset||c.total!=r.size||c.data.size()>static_cast<std::size_t>(max_bytes)||c.data.size()>static_cast<std::uint64_t>(r.size-offset)||c.eof!=(c.data.size()==static_cast<std::uint64_t>(r.size-offset))||(c.data.empty()&&!c.eof))throw content::ServiceError("invalid_storage","inconsistent chunk");
}
}
// The resource records a naming lookup only. Hash all assembled bytes before
// treating them as the requested content. A changed result invalidates assembly.
class Client {
public:
 explicit Client(std::string endpoint):endpoint_(std::move(endpoint)){}
 Client(std::string endpoint,ipc::Deadline deadline):endpoint_(std::move(endpoint)),deadline_(deadline){}
 Client WithDeadline(ipc::Deadline deadline)const{auto copy=*this;copy.deadline_=deadline;return copy;}
 Client WithServerExpectation(std::optional<ipc::ServerExpectation> server) const {auto copy=*this;copy.server_=std::move(server);return copy;}
 Client WithCancellation(ipc::CancellationToken token)const{auto copy=*this;copy.cancellation_=std::move(token);return copy;}
 content::OpenResult Open(const std::string& digest)const{
  if(!content_detail::digest(digest))throw content::ServiceError("invalid_request","canonical SHA256 required");
  auto transport=Transport();content::ContentReaderClient<ipc::FrameTransport> client(transport);auto result=client.Open(digest);
  if((result.outcome=="opened")!=result.resource.has_value()||(result.resource&&(!content_detail::resource(*result.resource)||result.resource->digest!=digest)))throw content::ServiceError("invalid_storage","inconsistent open result");return result;
 }
 content::ReadResult Read(const content::Resource& r,std::int64_t offset,std::int64_t max_bytes)const{
  if(!content_detail::resource(r)||offset<0||offset>r.size||max_bytes<1||max_bytes>65536)throw content::ServiceError("invalid_request","invalid read bounds");
  auto transport=Transport();content::ContentReaderClient<ipc::FrameTransport> client(transport);auto result=client.Read(r.handle,offset,max_bytes);content_detail::read(result,r,offset,max_bytes);return result;
 }
 content::CloseResult Close(const content::Resource& r)const{
  if(!content_detail::resource(r))throw content::ServiceError("invalid_request","invalid resource");
  auto transport=Transport();content::ContentReaderClient<ipc::FrameTransport> client(transport);return client.Close(r.handle);
 }
private:
 ipc::FrameTransport Transport()const{return (deadline_?ipc::FrameTransport(endpoint_,*deadline_,1u<<20):ipc::FrameTransport(endpoint_,5000,1u<<20)).WithCancellation(cancellation_).WithServerExpectation(server_);}
 std::string endpoint_;std::optional<ipc::Deadline> deadline_;ipc::CancellationToken cancellation_;
 std::optional<ipc::ServerExpectation> server_;
};
// Observes objects a store gains or loses. Every call is subject to the service's
// observe policy and per-object read policy; there is no automatic retry.
class Changes {
public:
 explicit Changes(std::string endpoint):endpoint_(std::move(endpoint)){}
 Changes(std::string endpoint,ipc::Deadline deadline):endpoint_(std::move(endpoint)),deadline_(deadline){}
 Changes WithServerExpectation(std::optional<ipc::ServerExpectation> server)const{auto copy=*this;copy.server_=std::move(server);return copy;}
 Changes WithCancellation(ipc::CancellationToken token)const{auto copy=*this;copy.cancellation_=std::move(token);return copy;}
 // An empty cursor starts at the current end. gap requires rebuilding from List.
 content::ChangePage Observe(const std::string& cursor,std::int64_t max_changes,std::int64_t wait_ms)const{
  if(cursor.size()>256||max_changes<1||max_changes>256||wait_ms<0||wait_ms>30000)throw content::ServiceError("invalid_request","invalid change request");
  auto transport=Transport(wait_ms);content::ContentChangesClient<ipc::FrameTransport> client(transport);auto p=client.Observe(cursor,max_changes,wait_ms);
  if(p.outcome!="page"){if(!p.changes.empty()||p.next!=cursor||p.at_end)throw content::ServiceError("invalid_storage","inconsistent change refusal");return p;}
  if(p.changes.size()>static_cast<std::size_t>(max_changes)||p.next.empty()||p.next.size()>256)throw content::ServiceError("invalid_storage","inconsistent change page");
  std::int64_t last=0;for(const auto& c:p.changes){if(c.sequence<=last||!content_detail::digest(c.digest)||c.size<0||(c.kind!="added"&&c.kind!="removed"))throw content::ServiceError("invalid_storage","inconsistent change entry");last=c.sequence;}
  return p;
 }
 // An empty continuation freezes a new snapshot; cursor is the change cursor at which it was taken.
 content::ListingPage List(const std::string& continuation,std::int64_t limit)const{
  if(continuation.size()>256||limit<1||limit>256)throw content::ServiceError("invalid_request","invalid listing request");
  auto transport=Transport(0);content::ContentChangesClient<ipc::FrameTransport> client(transport);auto p=client.List(continuation,limit);
  if(p.outcome!="page"){if(!p.objects.empty()||!p.continuation.empty()||!p.cursor.empty()||p.complete)throw content::ServiceError("invalid_storage","inconsistent listing refusal");return p;}
  if(p.objects.size()>static_cast<std::size_t>(limit)||p.cursor.empty()||p.complete!=p.continuation.empty())throw content::ServiceError("invalid_storage","inconsistent listing page");
  std::string previous;for(const auto& o:p.objects){if(!content_detail::digest(o.digest)||o.size<0||o.digest<=previous)throw content::ServiceError("invalid_storage","inconsistent listed object");previous=o.digest;}
  return p;
 }
private:
 ipc::FrameTransport Transport(std::int64_t wait_ms)const{
  auto t=deadline_?ipc::FrameTransport(endpoint_,*deadline_,1u<<20):ipc::FrameTransport(endpoint_,5000+static_cast<unsigned>(wait_ms),1u<<20);
  return t.WithCancellation(cancellation_).WithServerExpectation(server_);
 }
 std::string endpoint_;std::optional<ipc::Deadline> deadline_;ipc::CancellationToken cancellation_;
 std::optional<ipc::ServerExpectation> server_;
};
// A typed service outcome that ended Writer::Write.
struct WriteOutcome:std::runtime_error{
 std::string operation,outcome;
 WriteOutcome(std::string op,std::string out):std::runtime_error("storage: "+op+" "+out),operation(std::move(op)),outcome(std::move(out)){}
};
namespace writer_detail {
inline constexpr std::int64_t max_append=65536;
inline bool request(const std::string& r){return r.size()>=16&&r.size()<=128&&std::all_of(r.begin(),r.end(),[](unsigned char c){return(c>='a'&&c<='z')||(c>='A'&&c<='Z')||(c>='0'&&c<='9')||c=='_'||c=='-';});}
inline bool upload(const content::Upload& u){return !u.handle.empty()&&u.handle.size()<=128&&content_detail::digest(u.digest)&&u.size>=0&&u.received>=0&&u.received<=u.size;}
}
// Writer stages bounded uploads. Retain the request identity before Begin; a
// retry with the same identity resumes the live upload or returns its result.
class Writer {
public:
 explicit Writer(std::string endpoint):endpoint_(std::move(endpoint)){}
 Writer(std::string endpoint,ipc::Deadline deadline):endpoint_(std::move(endpoint)),deadline_(deadline){}
 Writer WithDeadline(ipc::Deadline deadline)const{auto copy=*this;copy.deadline_=deadline;return copy;}
 Writer WithServerExpectation(std::optional<ipc::ServerExpectation> server)const{auto copy=*this;copy.server_=std::move(server);return copy;}
 Writer WithCancellation(ipc::CancellationToken token)const{auto copy=*this;copy.cancellation_=std::move(token);return copy;}
 static std::string NewRequestId(){
  std::random_device random;static const char hex[]="0123456789abcdef";std::string id;
  for(int i=0;i<32;++i)id.push_back(hex[random()&15u]);return id;
 }
 content::BeginResult Begin(const std::string& request,const std::string& digest,std::int64_t size)const{
  if(!writer_detail::request(request)||!content_detail::digest(digest)||size<0)throw content::ServiceError("invalid_request","invalid write request");
  auto transport=Transport();content::ContentWriterClient<ipc::FrameTransport> client(transport);auto r=client.Begin(request,digest,size);
  const bool stored=r.outcome=="committed"||r.outcome=="present";
  if((r.outcome=="started")!=r.upload.has_value()||stored!=r.stored.has_value()||r.limit<0)throw content::ServiceError("invalid_storage","inconsistent begin result");
  if(r.upload&&(!writer_detail::upload(*r.upload)||r.upload->digest!=digest||r.upload->size!=size))throw content::ServiceError("invalid_storage","inconsistent upload");
  if(r.stored&&(r.stored->digest!=digest||(r.outcome=="committed")!=(r.stored->evidence=="hashed")||(r.outcome=="committed"&&r.stored->size!=size)))throw content::ServiceError("invalid_storage","inconsistent stored result");
  return r;
 }
 content::AppendResult Append(const content::Upload& u,std::int64_t offset,const std::vector<std::uint8_t>& data)const{
  const auto n=static_cast<std::int64_t>(data.size());
  if(!writer_detail::upload(u)||offset<0||n<1||n>writer_detail::max_append)throw content::ServiceError("invalid_request","invalid append bounds");
  auto transport=Transport();content::ContentWriterClient<ipc::FrameTransport> client(transport);auto r=client.Append(u.handle,offset,data);
  const bool counted=r.outcome=="out_of_order"||r.outcome=="too_large";
  if((r.outcome=="accepted"&&(r.received!=offset+n||r.received>u.size))||(counted&&(r.received<0||r.received>u.size))||(r.outcome!="accepted"&&!counted&&r.received!=0))throw content::ServiceError("invalid_storage","inconsistent append result");
  return r;
 }
 content::CommitResult Commit(const content::Upload& u)const{
  if(!writer_detail::upload(u))throw content::ServiceError("invalid_request","invalid upload");
  auto transport=Transport();content::ContentWriterClient<ipc::FrameTransport> client(transport);auto r=client.Commit(u.handle);
  if((r.outcome=="committed")!=r.stored.has_value()||(r.stored&&(r.stored->digest!=u.digest||r.stored->size!=u.size||r.stored->evidence!="hashed"))||(r.outcome!="incomplete"&&r.received!=0)||r.received<0||r.received>u.size)throw content::ServiceError("invalid_storage","inconsistent commit result");
  return r;
 }
 content::AbortResult Abort(const content::Upload& u)const{
  if(!writer_detail::upload(u))throw content::ServiceError("invalid_request","invalid upload");
  auto transport=Transport();content::ContentWriterClient<ipc::FrameTransport> client(transport);return client.Abort(u.handle);
 }
 // Uploads bytes and commits them; it never aborts. Service refusals throw WriteOutcome.
 content::Stored Write(const std::string& request,const std::string& digest,const std::vector<std::uint8_t>& bytes)const{
  const auto size=static_cast<std::int64_t>(bytes.size());auto begun=Begin(request,digest,size);
  if(begun.outcome=="committed"||begun.outcome=="present")return *begun.stored;
  if(begun.outcome!="started")throw WriteOutcome("begin",begun.outcome);
  const auto u=*begun.upload;
  for(std::int64_t offset=u.received;offset<size;){
   const auto n=std::min(writer_detail::max_append,size-offset);
   std::vector<std::uint8_t> part(bytes.begin()+offset,bytes.begin()+offset+n);auto appended=Append(u,offset,part);
   if(appended.outcome!="accepted"&&appended.outcome!="out_of_order")throw WriteOutcome("append",appended.outcome);
   offset=appended.received;
  }
  auto committed=Commit(u);if(committed.outcome!="committed")throw WriteOutcome("commit",committed.outcome);return *committed.stored;
 }
private:
 ipc::FrameTransport Transport()const{return (deadline_?ipc::FrameTransport(endpoint_,*deadline_,1u<<20):ipc::FrameTransport(endpoint_,5000,1u<<20)).WithCancellation(cancellation_).WithServerExpectation(server_);}
 std::string endpoint_;std::optional<ipc::Deadline> deadline_;ipc::CancellationToken cancellation_;
 std::optional<ipc::ServerExpectation> server_;
};
}
