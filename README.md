HTTPC
==

HTTP Client Library for security tooling.

## Features

- [x] promise based async interface
- [x] request rate control  
<br>

- [x] contextual information regarding http responses (request/response,timming,redirect chain, transport errors)  
<br>

- [x] support for automatic handling of cookies  
<br>

- [x] max redirect support
- [x] redirect loop detection
- [x] ability to prevent cross origin redirects
- [x] ability to prevent cross site redirects  
<br>

- [x] configurable cache busting with support for query & headers  
<br>

- [x] jitter option
- [x] option to replay ratelimitted requests
- [x] auto rate throttling based on 429 responses
- [ ] auto rate throttling based on ratelimit headers  
<br>

- [ ] ip ban detection
- [ ] apigateway based ip rotation (requires aws creds)  
<br>

- [x] Raw HTTP/1 requests
- [ ] Raw HTTP/2 requests
- [ ] Raw HTTP/3 requests
- [x] option to ignore ALPN and attempt HTTP/1 or HTTP/2
- [x] SNI injection      
- [x] CONNECT method support 
<br>
