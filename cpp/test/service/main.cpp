#include <abstraction/storage/client.hpp>
#include <iostream>
void check(bool b){if(!b)throw std::runtime_error("storage assertion failed");}
template<class F>void refuses(F f){bool caught=false;try{f();}catch(...){caught=true;}check(caught);}
int main(int argc,char**argv){try{
 if(argc==2&&std::string(argv[1])=="--help"){std::cout<<"consumer endpoint digest roundtrip|open|foreign|revoked|gap [handle size]\n";return 0;}
 if(argc<4)return 2;abstraction::storage::Client client(argv[1]);const std::string digest=argv[2],mode=argv[3];
 if(mode=="foreign"||mode=="revoked"||mode=="gap"){
  if(argc!=6)return 2;abstraction::storage::content::Resource r{argv[4],digest,std::stoll(argv[5]),"unverified"};
  auto result=client.Read(r,0,4);check(result.outcome==(mode=="gap"?"gap":"forbidden"));
  if(mode=="foreign")check(client.Close(r).outcome=="forbidden");if(mode=="revoked")check(client.Close(r).outcome=="closed");return 0;
 }
 auto opened=client.Open(digest);check(opened.outcome=="opened"&&opened.resource.has_value());const auto r=*opened.resource;check(r.verification=="unverified"&&r.digest==digest);
 if(mode=="open"){std::cout<<r.handle<<" "<<r.size<<'\n';return 0;}
 std::string bytes;for(std::int64_t offset=0;;){auto result=client.Read(r,offset,4);check(result.outcome=="data");const auto& c=*result.chunk;bytes.append(c.data.begin(),c.data.end());offset+=c.data.size();if(c.eof)break;}
 check(bytes=="content fixture bytes");check(client.Close(r).outcome=="closed");check(client.Read(r,0,4).outcome=="gap");
 abstraction::ipc::CancellationSource cancelled;cancelled.Cancel();refuses([&]{client.WithCancellation(cancelled.Token()).Open(digest);});refuses([&]{client.WithDeadline(abstraction::ipc::Clock::now()).Open(digest);});
 abstraction::storage::content::ReadResult bad;bad.outcome="data";refuses([&]{abstraction::storage::content_detail::read(bad,r,0,4);});bad.chunk=abstraction::storage::content::Chunk{0,r.size,{},true};refuses([&]{abstraction::storage::content_detail::read(bad,r,0,4);});
 std::cout<<"PASS bounded bytes, explicit unverified claim, release/gap, cancellation/deadline\n";return 0;
}catch(const std::exception&e){std::cerr<<e.what()<<'\n';return 1;}}
