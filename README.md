# NearTalk

Visit [neartalk.makeworld.space](https://neartalk.makeworld.space) to check it out! That server always runs the latest code on the `main` branch.

<!-- Copied from html/about.html -->

<h2>What is it?</h2>
<p>
NearTalk is chat platform to talk to people nearby.
</p>
<p>
Anyone with the same IP address is in the same chat room. For example, everyone
in your house will get the same chat room if they visit NearTalk. If you go to
your local coffee shop, everyone who visit NearTalk will be in the same chat room.
This extends to larger organizations like college/university campuses.
</p>
<p>
Depending on how the network is set up, all mobile devices using data with the same
network provider as you may be chatting together. Or similarly, all the other homes
using the same ISP. This is the minority of cases however.
</p>
<h2>Why is it?</h2>
<p>
For fun, mostly. I wanted to make a chat application and I wanted to use
<a href="https://htmx.org/">htmx</a>, and this seemed like a fun idea.
</p>
<p>
There are many reasons why NearTalk isn't useful, and talking to your fellow humans
face to face
is much better. However there are some times when having a local chatroom is useful,
like for discussing (or dragging) a presentation going on. At the end of the day,
I'm happy to have made something.
</p>

<!-- End copying -->

## Building

GNU Make is required to use the Makefile. Compiling with `make` automatically embeds version information into the binary from Git, and it's the only supported way to build the project.

Only the latest Go (1.17) is tested, but Go 1.16+ should compile.

## Deploying

You can look at the [neartalk.example.service](./neartalk.example.service) file in the repo as an example for running NearTalk under SystemD.

Currently the code does not handle TLS certificates, and so a reverse-proxy is required to use TLS and ensure user security. Make sure you set up your reverse-proxy so that websockets work as well. Just look up `<server name> reverse proxy websocket` to find a configuration.

Currently the code is also designed to work under a domain or subdomain, not a subpath.

Please let me know why you deploy your own instance if you do!

## License

NearTalk is licensed under the [AGPLv3](https://www.gnu.org/licenses/agpl-3.0.en.html). If host your own version, you must release your source code.
