HTTPC
==

HTTP Client Library for security tooling

## Features

- [x] promise based async interface
- [x] request rate control

- [x] contextual information regarding http responses (request/response,timming,redirect chain, transport errors)

- [x] support for automatic handling of cookies

- [x] max redirect support
- [x] redirect loop detection
- [x] ability to prevent cross origin redirects
- [x] ability to prevent cross site redirects

- [x] configurable cache busting with support for query & headers

- [-] jitter option
- [x] option to replay ratelimitted requests
- [-] auto rate throttling based on 429 responses
- [ ] auto rate throttling based on ratelimit headers

- [-] ip ban detection
- [ ] apigateway based ip rotation (requires aws creds)

- [x] Raw http requests
- [x] SNI injection      
- [x] CONNECT method support

- [ ] malformed HTTP/2 request support
- [ ] malformed HTTP/3 request support