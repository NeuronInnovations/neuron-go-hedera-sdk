Nov 13, 2025

## Nik-Viet \- Transcript

### 00:00:00

   
**Nikolaos Siafakas:** just okay it has started now just taking notes. Uh do you want to maybe share a little bit um share screen and uh see a little bit this documentation maybe maybe I can see something quickly to tell you ah that seems right or this seems wrong or  
**Do Hoang Viet:** Okay.  
**Nikolaos Siafakas:** things like that. Okay. Okay. Let's let's see.  
**Do Hoang Viet:** Yeah, I already already put the Go SDK uh repo inside the contract uh registry and ask AI to generate some documentations on how to yeah how to conduct the testing on the li P2P.  
**Nikolaos Siafakas:** Okay. Okay. Okay.  
**Do Hoang Viet:** also explain a little bit more about the architecture and also uh some knowledge about the hydro topics.  
**Nikolaos Siafakas:** Uh okay. I see that your uh agent is creating either mermaid or asi uh diagrams.  
**Do Hoang Viet:** Yes.  
**Nikolaos Siafakas:** Um um I like mermaid a lot myself.  
**Do Hoang Viet:** Yes.  
**Nikolaos Siafakas:** I I love mermaid. Uh uh it's a little bit.  
**Do Hoang Viet:** Yeah, that is still a little bit long long long to read.  
   
 

### 00:01:22

   
**Do Hoang Viet:** So I think I can make it all more comprehensive to uh for even a newbie to be able to reach out to our architecture.  
**Nikolaos Siafakas:** Yes. Yes. Yes. Yes. Yes. Um Oh, you know what? Go back. I saw something very nice. I saw something very nice. I saw something very nice.  
**Do Hoang Viet:** in the testing or the architecture.  
**Nikolaos Siafakas:** Who knows? It's fine. It's fine.  
**Do Hoang Viet:** Okay.  
**Nikolaos Siafakas:** It's fine. Um, you know what? Maybe it is also worth me telling you now. Uh, what I know is bad in in this SDK.  
**Do Hoang Viet:** I think the AI uh has explained it very clearly.  
**Nikolaos Siafakas:** The problem is that I don't believe the AI can see what I'll tell you what I don't think the AI can tell you what what is bad.  
**Do Hoang Viet:** So I think I can share these documents.  
**Nikolaos Siafakas:** The reason is that lip peer-to-peer is very poorly documented and if AI cannot read documentation then AI has bad opinions.  
   
 

### 00:02:51

   
**Nikolaos Siafakas:** I'll tell you what is bad. Uh yet let me let me see if I can let me share with you a little bit. Yeah. Uh come on let me share the screen.  
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** This is just just a general discussion now that you have this uh understanding what's going on here a little bit and let me share the entire screen with you. Give me a second. Where are you? Entire screen. There you are. That is me. The Okay, let me do this. Let me um find here the what is it called? No. Um, okay. The the way I normally Okay. No, that's fine. It's fine. Uh, okay. Give me one second. Can you see my screen? Yeah.  
**Do Hoang Viet:** Yes, perfect.  
**Nikolaos Siafakas:** This is stupid. Uh, okay. Okay, let me not do this here.  
**Do Hoang Viet:** Yeah, I'm asking AI to take down every bad things about the SDK to be something that was optimizing.  
   
 

### 00:05:21

   
**Nikolaos Siafakas:** Okay. So, I'll give you a little bit the history. Uh what what we what we did is uh yes, we wanted this peer-to-peer. And you know what? You know what? Let me uh maybe also bring this here up something from yesterday's uh presentation. But you understand that stuff already. Uh let me show you something of that presentation.  
**Do Hoang Viet:** Yeah, I think so.  
**Nikolaos Siafakas:** Um it's I've never used mirror.  
**Do Hoang Viet:** Good design.  
**Nikolaos Siafakas:** I don't know. I just just just did here something. Um yes one one one thing and maybe your um your AI is telling that is yeah you have here a buyer that is listening to our topic and you have a seller that's listening to our topic and then this guy says I want service and and then we establish the peer-to-peer connection that that's that's maybe the biggest trick we have in in our system that's maybe the biggest biggest trick and for for for it is it  
**Do Hoang Viet:** Yeah. Yeah.  
   
 

### 00:06:24

   
**Do Hoang Viet:** Yeah. Yeah. Very impressive.  
**Nikolaos Siafakas:** is it is okay in some things and not okay in some other things. And we we have this thing here. We have this guy here, the seller is dialing this buyer. Okay, that's what is happening. Now I'll tell you the bad things. Okay, the bad things is for instance if the buyer has not port forwarded uh things or not enabled UPN or is rooted in this and that then this dial will fail. Okay, that that's one thing. That's one thing.  
**Do Hoang Viet:** Yeah, that's correct.  
**Nikolaos Siafakas:** Now, now he he he he is now also the history limp peer-to-peer. This thing here we using the um specializing the specialtity is two things. What is the speciality of peer-to-peer? Specialtity of lip peerto-peer. Speciality of lip peer-to-peer. It's it's maybe maybe three things. Okay. One is uh come on this guys. One thing is um uh create multiple streams with another pier using the same port.  
   
 

### 00:07:53

   
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** So, so, so what what it means is that if you if you if one guy in lierto connects to another guy, you can create one stream or you can make 10 streams with the same guy, same port.  
**Do Hoang Viet:** Okay. Multiplex streams. Multiplex. Yes. Yes.  
**Nikolaos Siafakas:** Exactly. It's multiplex.  
**Do Hoang Viet:** And they they also use UDP base so for better net traversal than TCP.  
**Nikolaos Siafakas:** Yeah, exactly. Exactly. So, uh many connection protocols like quick and stuff like that.  
**Do Hoang Viet:** Yes.  
**Nikolaos Siafakas:** Okay.  
**Do Hoang Viet:** Yes.  
**Nikolaos Siafakas:** Uh sorry about the spelling. It's now what else? The the other specialtity of these guys is let let me wait for that. Uh oh yeah uh discovering we have this thing here.  
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** Okay. the DHT. They have that. Uh what what what other Here is a big specialtity they have a big specialtity they called.  
**Do Hoang Viet:** They have built in encryption.  
   
 

### 00:09:20

   
**Nikolaos Siafakas:** Yeah. Encrypted um here multiplex multi-lex encrypted strings. Okay. Fantastic. Uh, what else? They can whole punch. Peers can hole punch. Hole punching means that they have a way to get through the firewalls. So it doesn't matter if you don't have a port forward and this and that they have a way to uh get through firewalls.  
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** It's very good. Uh another specialtity which is related to this one. Just let me come on.  
**Do Hoang Viet:** Yes.  
**Nikolaos Siafakas:** Can I not create another? Is that it? That's fine. Let let me just do it like that. just copy paste. Uh another specialtity they uh uh they they can if they cannot if they cannot penetrate fires then  
**Do Hoang Viet:** Thank you.  
**Nikolaos Siafakas:** they can use a relay or proxy variable.  
**Do Hoang Viet:** relate. Yes.  
**Nikolaos Siafakas:** They call it a relay. It's a little bit like a proxy to have in the middle to help hole punch.  
   
 

### 00:11:16

   
**Nikolaos Siafakas:** Okay.  
**Do Hoang Viet:** already.  
**Nikolaos Siafakas:** They Yeah, they have that relay to help with the whole punch.  
**Do Hoang Viet:** Yeah, already seated noted in the architecture federated by AI.  
**Nikolaos Siafakas:** Okay, that's one good thing.  
**Do Hoang Viet:** Yes, very happy.  
**Nikolaos Siafakas:** Let Let's go. Another good thing.  
**Do Hoang Viet:** Yes.  
**Nikolaos Siafakas:** Uh they uh they can use the relay to proxy data between  
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** peers. So, so, so why is that good? Uh, why is that good? Uh, good.  
**Do Hoang Viet:** In case in case we can't get through the firewalls.  
**Nikolaos Siafakas:** Exactly. If firewall is strict. Okay. But it's also good for another thing. It's good because if a pier has not much upload bandwidth then it can give the data to someone with a better internet to distribute to the world.  
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** Okay, these are good things about PHP and the PHPM.  
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** Um, other good things, okay, they use EVM addresses, so sorry, EVM keys to create the the the peer ID keys.  
   
 

### 00:13:16

   
**Nikolaos Siafakas:** you know the EVM address we have the public key we have creates EVM addresses but also the IP uh peer ID addresses both addresses that coming from the same uh from the same key I mean we had that here somewhere in the documentation exactly so so so this is the key that lip peerto-peer is using and this is also I key that Hideera is using Hideera is using two or three but one of them is this  
**Do Hoang Viet:** Yeah, many cryptography uh schema support this.  
**Nikolaos Siafakas:** one. So when we try to use this type of key, okay, where we get this public key and from this public key, we get the P ID, we get the EVM address and maybe something else. But let's let's let's go a bit here back. That's the good stuff of about the uh lip here. That's all good. Let's talk about the bad stuff. And why don't we use the full of it? Um let me make here a B cycle here which is um somehow like that. And then it's okay.  
   
 

### 00:14:19

   
**Nikolaos Siafakas:** Okay, this is the bad stuff. Uh, let me put this here is next to this here. But it's because uh DHT is advertising your IP and EVM address to the air world.  
**Do Hoang Viet:** Yeah. Yeah. is actually terrible. That's why we are using HRA for R thing. Yes.  
**Nikolaos Siafakas:** Exactly. So now imagine imagine Yeah. imagine you say, "Oh, look, I'm a rich guy. Here is my EVM address. Uh there's 5 million uh bitcoins in this thing." Yeah, whatever. And uh this is my IP address. stupid. It's stupid to do that. Okay, sure you have to secure your computer, but it's stupid to to advertise uh crypto address with an IP address to put them together.  
**Do Hoang Viet:** Come to take my free money.  
**Nikolaos Siafakas:** Come, come to take my money, you know, if you can, but a lot of people can. Uh so that's one thing. The other thing about this DHD thing which is bad is um uh that if you use DHT you need to run a DHT server which means what does it mean?  
   
 

### 00:16:01

   
**Nikolaos Siafakas:** It means I run a DT server and I'm not only taking discoverable my IP addresses. I'm also a uh discovery point for uh other people, you know. So that's Viet address and that's Jim's address and this and that. So I have to have an open uh uh an open connection to people to come to check my DHT server. It's like running a DNS server.  
**Do Hoang Viet:** Yeah, expensive stuff. We will let Hedra to do that.  
**Nikolaos Siafakas:** Exactly. Exactly. I don't want to run a DNS server in my home uh with my Bitcoin wallet and stuff like that, right? Or I don't want to go to the airport and tell them, you know what, run a DNS server or a DH DHT DNS. It's it's pretty much the same if you if you see it as a technology because these guys, they say, look, we are running an airport. We are not here to do the discovery of your Bitcoin b\*\*\*\*\*\*\*. Okay, this is very very bad with the DHT and that's why we don't use the DHT.  
   
 

### 00:17:01

   
**Do Hoang Viet:** Yeah, exactly.  
**Nikolaos Siafakas:** That's why we don't use the DHT but it comes with consequences. So so so here is um everything else is great in litomia. All that stuff I love all that stuff. Okay, here is a problem.  
**Do Hoang Viet:** That's  
**Nikolaos Siafakas:** When you say ah you know I don't want this DHT anymore I go with hideera say yeah okay you go with hideera no problem but you lose some of the benefits one of the benefit is uh this here gone uh Oh, gone. Uh, not fully gone.  
**Do Hoang Viet:** Was it James idea to migrate to Hedra?  
**Nikolaos Siafakas:** To make to No, no, no. is not technical enough to to to to make this um um he's very technical but not technical in the sense to make this this migrations. That was that was actually my idea uh to to do that. But it comes with consequences. Whenever you you don't use something of a library, it comes with consequences. But here's the thing. We we need to to work through these consequences.  
   
 

### 00:18:42

   
**Nikolaos Siafakas:** We need to understand why did we lose that ability and how can we recover from that in the future. Now, now you see we you you can't why why why did why did we lose this ability? You may ask. This ability is lost because Oh, wait a minute. And we lost this one back. This ability is lost because uh yeah, of course this one we lose this one. We lose all the things here. Uh yeah, we only have that here. We we uh simple punch. Simple hole punch. Uh if you lose this ability here, uh you lose all the other abilities. Now, why did we lose this ability? We lost this ability because lip peer-to-peer has this ability to run in relay mode. But the relay itself is using the stupid DHT.  
**Do Hoang Viet:** Oh.  
**Nikolaos Siafakas:** Okay. The hole punch that the relay is using uh we wrote a small hole punch. That's why I keep it still still still still thing still still wide.  
   
 

### 00:20:06

   
**Nikolaos Siafakas:** But our hole punch doesn't work well. Let's let's put it like like that.  
**Do Hoang Viet:** Yeah, I see. So we are done with the uh strict firewalls.  
**Nikolaos Siafakas:** Yeah, there is something but we didn't do it well. We tried to you see because we lose this ability of the relay and lip pure is using the relay to do the hole punch. We lost completely the relay node because we don't have the DHT. So we cannot use their hole punching ability. So we have written something for ourselves but it's not good. Now how do we work now from from from here on? Yes, we can we cannot use the DHT. We just cannot use it. Is the trick here really to look at the relay of liptopia. See how it works. And yes, we want all this relaying thing. We want all this moving the the data back and forth. We want that through relay and we want it to to be used as a proxy but we al also want to to make this hole punching work.  
   
 

### 00:21:12

   
**Nikolaos Siafakas:** Um and this is the question now shall we look into this relay maybe and see can we imitate what it's doing so that we do it with hideera because because uh hole punching is um is something that's doable without litoe you just need to know how you just need to know the protocol or or do we look again at leeer and we say hm okay maybe in this case we can use or maybe actually doesn't use DHT. Maybe Nick, you get it wrong. You don't have to use DHT uh for the uh relay. Maybe that is true. Maybe the relay has a way to switch it off and doesn't need it at all. Maybe it's true, but it doesn't need it at all. Maybe we can look at that. And to be honest, I I looked a little bit. I thought that this is integral to the uh relay. Maybe it's not. Maybe it did misunderstand that. And this is now something about uh the documentation of liptopia.  
   
 

### 00:22:17

   
**Nikolaos Siafakas:** If you try to find documentation, you won't. If you create it yourself with your agents, you'll find some. Now, now that's one thing. That's one. This is a biggie. We want this relay. We want this relay.  
**Do Hoang Viet:** So, have we gave it a try to make a clone of the relay system?  
**Nikolaos Siafakas:** We want No, we have not. And and let me tell you this is okay. This is not a bad thing about uh about the SDK. We know that uh the big bad thing is that whole punch hole punching hole punch is not uh uh reliable right now. Okay. Big one. Uh can I change the shape of that? It's fine. Another big one. Uh we don't have proxies or it is it is big because I'll give you an example.  
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** Let me let me go to this uh documentation. Maybe I have it here. Okay, I give you an example.  
   
 

### 00:23:37

   
**Nikolaos Siafakas:** Okay, we have these guys here. This is an airport. Okay, there's one, two, three, four sensors here. One, two, three, four sensors here. And they and they they talk and they buy data from these guys, they're running a buyer here and all these guys are sellers. Very good. All these sellers, they run inside a um home network. Look, we we producing around 100 kilobyte a second for for the road data. And you may say 100 kilobyte is not a lot, Nick. It's a lot. It's a lot. Most people have, you know, one megabs upload effectively at at least in the UK. And from this one megabs upload, if you go into a Zoom call or something like that, then there is nothing left. So now when when two people want to get data from this guy not only this airport or three or four or five people this guy cannot serve five 10 people you know if he's on a zoom call or something or if his children are watching uh Netflix okay so so you may say okay this is download doesn't matter it affects the upload too a lot uh so the bandwidth is getting limited that's why it's good for instance to have a relay somewhere a big proxy that's nearby and says look I am big I am having a 100 megabs upload if you cannot you give me the stream you give me the stream and and via  
   
 

### 00:25:10

   
**Nikolaos Siafakas:** me we give it to the to them I'll give you the good news I'll give you here good news bad news good good news up The  
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** good news is that liptopia has an addressing scheme that goes like that. It can look like that. uh of so let's say you want to to reach the sensor number one this is the EVM of address of sensor number one then you you write here forward slash I think something called circuit Um it's something like that in the documentation. So you can say I I I want the this data from this sensor but using this proxy. So the system there lito communicating uh with the sensor communicating with the proxy the sensor is sending it to the proxy and you get it from the proxy good news bad news we don't use that we don't use these addresses because we don't use the relays at yet. Give me a second. I need to go to the L for just one second. Yeah.  
**Do Hoang Viet:** Yeah, sure.  
   
 

### 00:27:12

   
**Do Hoang Viet:** Just take your time.  
**Nikolaos Siafakas:** Sorry, I'm back. Okay. Where we where we Yes, they have these addresses. We don't use them because we don't have the relay right now. And you know what?  
**Do Hoang Viet:** Yes.  
**Nikolaos Siafakas:** The relay the the relay is is is getting required. And here's the other bad thing we have in SDK. We don't have a way a sensor for instance. Okay. Right now you go to a server and you say I want data and he'll say no problem I will try. Doesn't matter who whoever comes. Yeah. I'll give you data. No problem. This can be a problem. Of course, if you don't have the bandwidth to do that, you're not going to survey anyone. Okay?  
**Do Hoang Viet:** Right.  
**Nikolaos Siafakas:** You you you promised this airport, you're going to give them data and now many many more people want your data and and you're not serving anyone because your system is just overloaded now.  
   
 

### 00:31:13

   
**Nikolaos Siafakas:** So, so for instance, ideally what you want is, you know, uh white list one three direct connections and one proxy connection  
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** for instance and no one no on else you know for instance you want to do that you say look I'm going to give this direct connections to these guys, to this airport, to that guy and the other guy. But everybody else needs to use a proxy connection. Okay? I'm not going to give you direct data at all. Don't come to me. Come via the proxy because I'm giving I'm giving out four streams, one for three direct and one proxy stream. So, everybody else go to the proxy. That would be the ideal thing. Another bad thing about the SDK is  
**Do Hoang Viet:** one question. So, uh what kinds of structures are we using to serve for uh other people's like first come first serve or Okay.  
**Nikolaos Siafakas:** right now just just cowboy random. Yeah, you you come I'll try. So so so it is it is you know fun out.  
   
 

### 00:32:30

   
**Nikolaos Siafakas:** Um you you get you have a data packet and there is five guys connected to you and you're trying to make five copies. Another data packet five copies another data pack five copies which is fine if you have the bandwidth.  
**Do Hoang Viet:** Yes. So, it's completely random.  
**Nikolaos Siafakas:** Exactly. And because you just just touched on that, here is another thing here. Right now the the SDK uh uh if you have a slow connection between pier one and pier two. and a fast between pier one and pier three then the whole system of pier one degrades to the slow connection that's a deficiency it happens so so so you are talking to five people okay and the one of the guys is is has a slow connection. Okay, the connection between you two is slow now. The whole system because it's not maybe I did fix it at some point. I don't remember but I remember it was a problem. The whole system was degrading to the slowest guy.  
   
 

### 00:34:02

   
**Nikolaos Siafakas:** Okay. So because you know you know where where you do this copy copy this to this guy, copy that to the other guy, copy that to the other guy. Uh so so when you copy now when you in the process of copying the packet to one guy that is slow of course everybody else is waiting until you're done with that guy. So there is not good multi-threading in there.  
**Do Hoang Viet:** So, It actually set the bandwidth limit to the lowest person in the group.  
**Nikolaos Siafakas:** It's it's not it is bad coding. It's not really a bandwidth limit. It's bad coding. Good coding would run that in in multiple threads and the slow guy lives in the in the thread alone and the the fast guys are not affected right now because it's not multi-threaded, not done with Golang channels and stuff like that. If I'm busy copying to the slow guy, I'm not in different threads to to deal with his fast guys.  
**Do Hoang Viet:** Yeah, right. Single threat.  
**Nikolaos Siafakas:** Yeah, right now it's down simple threaded.  
   
 

### 00:35:12

   
**Nikolaos Siafakas:** But here's also why why it's good news. Good news is because they have a go. We have it in Golang and it would be even better in Rust. Okay. So, so this library is very very fast. This SDK it's really really fast. If you write it, we can write it in a JavaScript, but it's not going to be serving uh aviation. I can't tell you that. So, so a lot of people say Golang, you know, but but I'm not but but for this thing, it needs to be really really fast. It really needs to be fast. You know if you if you run that library yourself and connect let's say to to connect to to 30 to 30 sensors. I'm not saying 200\. Connect to 30\. You will see your machine is going to go like no do that in JavaScript.  
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** It's not going to even start. It's not going to even start.  
**Do Hoang Viet:** Yes. Yeah. Because rest because uh because R offers robots and safe mechanisms for multi threaded programming.  
   
 

### 00:36:23

   
**Nikolaos Siafakas:** Uh exactly. It's multi. That is the key point. It's multi-threaded. Uh both Rust and K and Golang. They are the they're the they're really good at creating multiple threads like like Java and stuff like that. It's just Java is too big to run inside Raspberry Pi. And they're getting better now, but until until they make it better, good luck.  
**Do Hoang Viet:** Yes.  
**Nikolaos Siafakas:** I mean, the best would be Rust, but the problem with Rust is that um not many people can code that. So So Golang is a good compromise. Now, now let's go here also because we we we're talking about languages. Yeah. If you go here to lip peer-to-peer, you'll see that these guys are are creating this library in many many many languages. So let's say in in the future if you want to say okay let's do I don't know JVM or Python or this or that okay they tell you here it's all in progress nothing is done can you see  
**Do Hoang Viet:** Yeah.  
   
 

### 00:37:25

   
**Nikolaos Siafakas:** this That's Yeah.  
**Do Hoang Viet:** Yeah, we we actually have many good uh R developers in in Vietnam because we need to get familiar with uh Rust to do things with uh other blockchain like NI or Solana.  
**Nikolaos Siafakas:** Yeah. Yeah. Yeah. Of course. Yes. But uh this is now not this is deep rest, not just bit of blockchain smart cont.  
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** And not only that, look at that. You see with every language, they don't have all the features developed. For instance, look the quick protocol in Rust. It's not done. It's done only in Go in Go.  
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** It's not even going to happen in in in in JavaScript for instance. Some things I hear that work in the browser. Some things work in node, you know, is is you know even they say that this is completely unimplementable. Quick cannot do that. uh here it's not even started you know is is not feature complete everywhere the most complete version is golang and second best maybe JavaScript maybe  
   
 

### 00:38:39

   
**Do Hoang Viet:** Yeah. So I think is is communitydriven. So is it is it community driven like or they have a uh in-house developers?  
**Nikolaos Siafakas:** so That's one thing. Yes. Yes. Yes. It used to be developed by Proto Collapse and Protocol Collapse are the guys who who've done the IPFS and then they gave that project to the community and say, "Okay, here's a little bit of money. You  
**Do Hoang Viet:** Yeah, I got it.  
**Nikolaos Siafakas:** run that now. and the documentation just just just doesn't exist. I mean, let me show you here. Okay. Uh well, for no, let's go let's go to Yeah, there's some documentation, but it won't help you. Let me go here. Come on. Where is that thing? Uh there is a forum. Okay. Uh uh actually even today you can go and talk about ah there you can go to the forums and chat. Yeah let's go look at that.  
**Do Hoang Viet:** Okay.  
   
 

### 00:40:12

   
**Do Hoang Viet:** Yeah, they have research and favor discussions.  
**Nikolaos Siafakas:** Not only that, okay, not only that, people people ask for uh for for things, you know. Okay, this is a pinned post. That's fine. But people ask people ask for things and nobody ever replies. Nobody ever replies for anything, you know. Uh, nobody nobody looks connect.  
**Do Hoang Viet:** Just look by a abandoned house.  
**Nikolaos Siafakas:** Hello. How do I do that? You know, and this and nobody.  
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** Nobody.  
**Do Hoang Viet:** Not very active.  
**Nikolaos Siafakas:** I don't know what's going on, you know. So, so if you have a question, don't even waste your time to ask the community. You can go to they have a telegram channel if I'm in you can ask there nobody knows how to answer or anything straight ahead and also talk to each other to for the experiences  
**Do Hoang Viet:** Yeah. So I think we we just go straight ahead to chat ab Yes.  
**Nikolaos Siafakas:** uh for instance this multipplexing thing it took me some some some some while to understand okay he is a good think you know they say multiplexing encrypted string streams and here's a bad thing in the SDK we use only one stream from one pier to another pier we using only one stream you may see for instance uh things that called something ID is B2.0 zero stream.  
   
 

### 00:42:01

   
**Nikolaos Siafakas:** Okay, we're using only one stream between one guy to another one. In the node builder, it was called something n something. But this thing can do multiple streams. But the way we have the SDK is is is is made to create one connection, open a stream and keep that stream open forever. So you will see that the SDK is trying to keep a stream open forever. Now, that is something that the documentation didn't didn't wasn't really clear about. And and here here is a good thing about about uh about uh leerto. I did not know that. Now I do. And closing a stream is very cheap. Of course, it's not a bad thing about uh uh it's not a bad thing, but creating a connection is effort. You know, creating a connection, find IP addresses, this that handshake, it's it's it's tough. But opening and closing a stream once you have a connection doesn't cost anything. It's very very fast, very quick, very this, very that. in my m mind because there was no documentation I thought that even opening streams and closing them are uh difficult and I'll tell you now why I'm I'm telling you that and you tell me when you need to leave yet because I can have that discussion forever you see you see in in in liperto everything is just a stream okay here is a stream there you go and if you want to to to to send let's  
   
 

### 00:43:53

   
**Nikolaos Siafakas:** say uh 3J JSONs uh JSON one you can send it here and then and then you can send another here. Okay. Uh that's what you can do. Let's write here a small arrow. You can do this. You can open stream here and you can close stream here and this is how how we are doing it in the SDK and it's not a good thing. It's not a good thing because we trying to to to keep that stream always open and we really really trying hard and all this kind of things, but we we didn't know that it's really cheap once you have a connection. It's really cheap to open and close a stream. We could do this instead. It wouldn't make a difference at all.  
**Do Hoang Viet:** Yeah. Make make different stream for more driven different purpose.  
**Nikolaos Siafakas:** Exactly. Exactly. Exactly. You know where I'm coming from. So, I'm not going to do that. We could do that.  
   
 

### 00:45:15

   
**Nikolaos Siafakas:** Uh we could do that. Open, close, open, close, open, close. Not a problem. We not doing that. and and the way we written the the the uh SDK is that we don't have control of opening and closing the stream is the SDK doing that but we should give that control to the users of our SDK. We should give the control to open and close stream. Why why is that important? Because because that stream here lives inside the connection. Let me do this here. Let me send it to back. Okay, let's train the list collection.  
**Do Hoang Viet:** Yeah. So, so we can save the bandwidth, right?  
**Nikolaos Siafakas:** Not maybe maybe you see to be honest in ADSB in ADSB it's okay to do it like that because there is no gaps really it's like going there is no gaps but with other things like um get the what is the sensor location you know it's maybe a request per second a request per minute maybe there is no point open keep trying to maintain that stream why because you see these things here they live inside a connection When the connection breaks, of course, everything breaks.  
   
 

### 00:46:50

   
**Nikolaos Siafakas:** And right now in our SDK, when the stream breaks which can break the stream can break and the connection may still be there.  
**Do Hoang Viet:** question. Yes.  
**Nikolaos Siafakas:** The stream can break. It can disappear and the connection can still be there. We are not trying to okay open close stream again because we don't have this um code what we try to do you know what make it all go away everything go disappear go away everything everything you know make it disappear start again with a hideera request and start everything again from scratch why why do we do that why do we do that the reason we do that is yeah one yeah bad code and not understanding and the reason we do that is that uh we don't remember IP addresses and this this is going to send me out to the first task we don't remember the IP address in our system so so so when somebody disappears or when we disappear and we come back reboot and we know that we are still sorry we don't know But imagine we knew that we still need to be talking to somebody.  
   
 

### 00:48:10

   
**Nikolaos Siafakas:** We don't know his IP address to to just dial him, you know, straight away without if you know the IP address for this guy, you know it.  
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** Okay, it may change, but they don't change like every second. Okay, it may change every five days, whatever. Mine is there forever the same. uh you see they they don't change the things that easily if you can remember the IP address and you can connect to this guy again you don't need to go talk to Hideera and start all this back port I want your service I want this I want that our system right now does that whenever a connection is lost goes to Hideera says I lost the connection let's start all over again and the other guy says oh my internet is broken let's try again Hideera this Hideera No, if you have the IP addresses, forget about the Hideera, you know. So, Hideera was there really to Exactly.  
**Do Hoang Viet:** Can can we put the laser the IP to somewhere like database or catch system?  
   
 

### 00:49:16

   
**Nikolaos Siafakas:** So, task number one, remember the IP address. So remember the IP address so that when we reboot at least we know it that is the the number one priority.  
**Do Hoang Viet:** Okay, I think we can use Reddit in this case.  
**Nikolaos Siafakas:** Now now now here is uh here is where I need you to be careful to research this system right now. this SDK. If you go to GitHub, go to go to to this challenge I've put for the uh for the hackathon.  
**Do Hoang Viet:** Yeah, I remember you mentioned that we we we will need to make it uh very small enough to put it into the device.  
**Nikolaos Siafakas:** Exactly. Look, look at the for the sky and blood challenge. Look at the main file. This is it. This is it. this thing here uh uh in this case sorry it's it's receiving data but but I can create a mind file like that that sends data out of jet vision that's really 73 lines of code I can do that I can do that and we have for this guy here somewhere which is a little bit bigger but I can make it as small like this one because this it's not going to add more data and this uh SDK I'm downloading here uh where is it this one is incredibly small it's it's a runtime of 30 megabytes three sorry 30 now if you bring a radius in here it's going to be already gone  
   
 

### 00:50:49

   
**Do Hoang Viet:** Listen.  
**Nikolaos Siafakas:** you see you need to find something small maybe maybe radius is 2 megabytes I don't think so I think it's too big I have looked before at a system called Bolt DB.  
**Do Hoang Viet:** Cool.  
**Nikolaos Siafakas:** I'll show you. I I've tried that before. I never finished that job. Bold DB. B DB. Okay, let me find it. Is it this one? No. this one.  
**Do Hoang Viet:** Is it some kind of portable database?  
**Nikolaos Siafakas:** Uh I'll show you uh it wasn't an archive but anyway it's somewhere. Uh this is an embedded database. It's very very very small and it's it's used it's the database of Kubernetes you know things like that. I'm not telling you to use that. I'm not telling you to use that but but but something similar.  
**Do Hoang Viet:** Okay.  
**Nikolaos Siafakas:** This is a very simple um key store, you know, key value, very very similar. Uh I don't remember why I didn't use it in the end.  
   
 

### 00:52:28

   
**Nikolaos Siafakas:** Um I really don't remember. But yes, that's what we that's what we want. We want a very very small database. Very very small database that doesn't really cost too much in memory. And I'll tell you what I did in the end. What I did in the end is I wrote um uh I I I created a file. I created a file somewhere. Come on here. I don't know. I created a file, you know, and I was uh you know it was a JSON and I was writing into the file. I was doing that. that I was writing inside the file IP address is this that you know what happened that file got corrupted because when a device was shutting down while I was writing to the file the file was corrupted and because the file was corrupted the whole program was uh not launching and the brilliant thing is But there were 60 devices already out there. All of them corrupted file. All of them.  
   
 

### 00:53:46

   
**Nikolaos Siafakas:** And the whole network is down.  
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** So, so, so the lesson is if you think you are clever and you can make a database yourself with a file, think twice because it's not easy.  
**Do Hoang Viet:** Exactly.  
**Nikolaos Siafakas:** So, so, so no, I I strongly disenourage making it u by hand. I strongly encourage find the smallest thing you can get that is enough and uh doesn't take too much space, but it's also easy to to to read to interrogate. Uh that is the the first thing. Remember the stupid IP addresses. The other thing is which is very related Where do you find the IP address? Oh yeah, you know it's coming from Hideera. Yeah. So there is code in the SDK that's listening to the topic and something comes in and then there's IP address. Ah cut cut it. Yeah. Here is the other problem we have in remembering. When we read something from a topic, we don't remember it. It disappears. Okay?  
   
 

### 00:55:09

   
**Nikolaos Siafakas:** We we we read things from the topic, we put it in some data structure, then we reboot. It's all gone.  
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** The other problem is now on top of that even without rebooting we do not look deep into the topic we are only looking you know at the last item so for instance when I reboot when I reboot I know nothing about nothing I know no IP addresses I don't know if somebody if I was you know if I was um uh uh streaming with somebody 5 minutes ago I don't know that I don't know that you know I don't know that I am in the middle of a request and I had to reboot I don't know that because yeah that information came into into the topic I looked at it I acted and then I started rebooting and now I forgot about that and now I'm expecting the other party to come hey what's happening Man, let's start a new Hideera request. Start the service again. You know, cost a lot of money.  
**Do Hoang Viet:** Yeah.  
   
 

### 00:56:28

   
**Nikolaos Siafakas:** Why?  
**Do Hoang Viet:** Right.  
**Nikolaos Siafakas:** Because we don't remember things. Another problem because we don't remember things. The buyer I I will write this down. Maybe ask to write this problem problems down. Another problem because we don't remember things. The buyer, if you look at the code, you will see that whenever he's trying to talk to another guy, he's creating a shared money account between these two guys. Put some money in it and then they start talking. The problem is that the buyer when he reboots, he forgets what shared account he has with the other guy. It's gone. So whenever he tries to talk to him again creates a new shared account and and shared account creations are really really really expensive. If he remember that ah I have a shared account with this guy and I always have the same shared account whenever whenever Nick talks to Viet it's always this one shared account that's between Nick and Viet that's managed by buyer. I have to keep that forever.  
   
 

### 00:57:36

   
**Nikolaos Siafakas:** I don't want to create a new one every time. Why do I create new ones? Because I don't have a database and I don't remember things. That's why. So So if you see you see because we don't have state because we don't remember uh we we are paying the penalty for that. Is there a good thing about not having state? Yes, there is a good thing about not having state because for instance in jet vision device they there is no hard disk there is a sand disk um SD card and if you write too much into that SD card you're going to burn it okay so stateless is great and the question is okay can we get the state from somewhere else maybe not from hard drive I've looked into that yet a lot of state is inside  
**Do Hoang Viet:** Yes.  
**Nikolaos Siafakas:** the blockchain too but things like IP addresses we cannot uh store in the blockchain easily. It is there already. It's in the topic. Yeah, you may start digging deep into topic to find that.  
   
 

### 00:58:45

   
**Nikolaos Siafakas:** That's you can do that right now. You can say to find the IP address of somebody. I don't need to go to the um to the database. I can go into my input topic and see who asked for requests this day and the day before.  
**Do Hoang Viet:** Yeah, but it but it's really resource consuming.  
**Nikolaos Siafakas:** and I can find messages. Yeah. Yeah, that's a good solution. Actually, a small problem is that what happens if the blockchain is down that moment? Because the blockchain is down a lot of times. Say, what? Yeah, no, no, the blockchain is there, but they say, yeah, yeah, the blockchain is there. It's only the access of the blockchain that's broken. Yeah, sure. So you if I cannot access it mate it's not there so so so that's that that's that's how it works.  
**Do Hoang Viet:** Thank you.  
**Nikolaos Siafakas:** Um but no it's Yeah.  
**Do Hoang Viet:** It's you usually down.  
**Nikolaos Siafakas:** Yeah. So task are remember things that have come from a hideera topic.  
   
 

### 00:59:53

   
**Nikolaos Siafakas:** Remember them. How far do we remember them? one day worth of to messages maybe one day worth of messages and then be able to to recall can I looking now into my database go to that message get the IP address from that you know uh this is unfinished business try to connect again like that don't ask hideera again don't send again to hideera hey let's try to reconnect this that is the first thing remember create a database that remembers hideera topics the database needs to be small enough and needs to be very careful with not writing too many times. Write only when you have to, not just write in a loop, you know.  
**Do Hoang Viet:** Yeah.  
**Nikolaos Siafakas:** Uh I think we are 10 minutes over time. I know you want to go home. Uh let's have a look at let's see if I can transcribe some of these messages here.  
**Do Hoang Viet:** No worries. Can we just uh export the whole diagram you wrote and have it as the context for AI later?  
   
 

### 01:01:01

   
**Nikolaos Siafakas:** Oh yeah. Uh let me just share it here with you. Uh here you go.  
**Do Hoang Viet:** Okay, perfect.  
**Nikolaos Siafakas:** It shed you.  
**Do Hoang Viet:** See Uh one question. So, so can we set an interval for for a device to write the list of AP IP to the database like um for each 10 minutes we can or seconds we can write all the IPs address to somewhere  
**Nikolaos Siafakas:** M maybe I'll tell you a different way we can do it can be reactive lip peer-to-peer has a system called the event bus and inside the event bus it's telling you if something has changed for instance it's telling you if somebody's IP address has changed and you say okay let's do it uh let's only write when when when when something has changed or let's only write when I get a new request from the uh blockchain so somebody's got a service request the IP is there encrypted only write for instance then uh you know what to be honest it's it's wrong for me to tell you how to do that I think I use your own is your own intuition on which which one is the best way to do that so that we don't write too much goodbye  
   
 

### Transcription ended after 01:03:14

*This editable transcript was computer generated and might contain errors. People can also change the text after it was created.*