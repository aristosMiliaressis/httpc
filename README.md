HTTPC
==

HTTP Client Library for security tooling

## Features

- [x] contextual information regarding http responses (request/response,timming,redirect chain, transport errors)
- [x] support for automatic handling of cookies
- [ ] implement cookie scope handling
- [ ] implement cookie expiration handling

- [x] max redirect support
- [x] redirect loop detection
- [x] ability to prevent cross origin redirects
- [x] ability to prevent cross site redirects

- [x] rate throttling option
- [x] jitter option
- [x] auto rate throttling based on 429 responses
- [ ] auto rate throttling based on ratelimit headers
- [x] option to replay ratelimitted requests
- [ ] built in concurency support

- [ ] ip ban detection
- [ ] apigateway based ip rotation (requires aws creds)

- [x] configurable cache busting with support for query & headers

- [x] Raw http requests
- [x] SNI injection      
- [ ] CONNECT method support
