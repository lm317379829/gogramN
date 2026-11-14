package telegram
import("path/filepath";"testing")
func TestCacheUnknownThenKnownOwner(t *testing.T){
 c:=NewCache(filepath.Join(t.TempDir(),"user.cache"));defer c.Close()
 if err:=c.BindToUser(0);err!=nil{t.Fatal(err)}
 if err:=c.BindToUser(42);err!=nil{t.Fatal(err)}
 if c.InputPeers.OwnerID!=42{t.Fatal("unknown user prevented later owner binding")}
}
func TestCacheRejectsOtherOwner(t *testing.T){
 dir:=t.TempDir();storage:=NewFileCacheStorage(filepath.Join(dir,"user_42.cache"))
 if err:=storage.Write(&InputPeerCache{OwnerID:99,InputUsers:map[int64]int64{777:888}});err!=nil{t.Fatal(err)}
 c:=NewCache(filepath.Join(dir,"user.cache"));defer c.Close()
 if err:=c.BindToUser(42);err!=nil{t.Fatal(err)}
 if c.InputPeers.OwnerID!=42||len(c.InputPeers.InputUsers)!=0{t.Fatalf("foreign peers survived: %+v",c.InputPeers)}
}
func TestCacheUsesExistingUserFile(t *testing.T){
 dir:=t.TempDir();storage:=NewFileCacheStorage(filepath.Join(dir,"user_42.cache"))
 if err:=storage.Write(&InputPeerCache{OwnerID:42,InputUsers:map[int64]int64{777:888}});err!=nil{t.Fatal(err)}
 c:=NewCache(filepath.Join(dir,"user.cache"));defer c.Close();if err:=c.BindToUser(42);err!=nil{t.Fatal(err)}
 if c.InputPeers.InputUsers[777]!=888{t.Fatal("existing user cache not loaded")}
}
