#pragma once
#include <abstraction/storage/content/rec.h>
#include <abstraction/ipc/frame.hpp>
#include <algorithm>
#include <optional>
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
}
