
## Protocol

Every GET/POST request should check the IP address and make sure that there are never more than 2 IP addresses using a single channel. Once two IP addresses are in, then the channel is *full*.

1. **Sender** requests new channel and receives empty channel from **Relay**, or obtains the channel they request (or an error if it is already occupied).

    POST /join
    {
        "channel": "...", // optional
        "curve": "pxxx", // optional
        "role": "sender"
    }

2. **Sender** generates *X* using PAKE from secret *pw*.

3. **Sender** sends *X* to **Relay** and the type of curve being used. Returns error if channel is already occupied by sender, otherwise it uses it.

    POST /channel { "x": "..." }
    Note: posting to channel always requires UUID and channel for validation.

4. **Sender** communicates channel + secret *pw* to **Recipient** (human interaction).

5. **Recipient** connects to channel and receives UUID.

5. **Recipient** requests *X* from **Relay** using the channel. Returns error if it doesn't exist yet.

    POST /channel   (returns current state)

6. **Recipient** generates *Y*, session key *k_B*, and hashed session key *H(k_B)* using PAKE from secret *pw*.

7. **Recipient** sends *Y*, *H(H(k_B))* to **Relay**.

    POST /channel   { "y": "...", "hh_k": "..." }

8. **Sender** requests *Y*, *H(H(k_B))* from **Relay**.

    POST /channel

8. **Sender** uses *Y* to generate its session key *k_A* and *H(k_A)*, and checks *H(H(k_A))*==*H(H(k_B))*. **Sender** aborts here if it is incorrect.

9. **Sender** gives the **Relay** authentication *H(k_A)*.

    POST /channel { "h_k": "..." }

10. **Recipient** requests *H(k_A)* from relay and checks against its own. If it doesn't match, then bail.

    POST /channel

11. **Sender** connects to **Relay** tcp ports and identifies itself using channel+UUID.

12. **Sender** encrypts data with *k*.

13. **Recipient** connects to **Relay** tcp ports and identifies itself using channel+UUID.

14. **Relay** realizes it has both recipient and sender for the same channel so it staples their connections. Sets *stapled* to `true`.

16. **Sender** asks **Relay** whether connections are stapled.

    POST /channel

17. **Sender** sends data over TCP.

18. **Recipient** closes relay when finished. Anyone participating in the channel can close the relay at any time. Any of the routes except the first ones will return errors if stuff doesn't exist.

    POST /channel { "close": true }





# Notes

https://play.golang.org/p/1_dfm6us8Nx

https://git.tws.website/t/thesis

https://github.com/tscholl2/siec

*croc* as a library

- use functional options
- every GET/POST request should check the IP address and make sure that there are never more than 2 IP addresses using a single channel
https://medium.com/@simplyianm/why-gos-structs-are-superior-to-class-based-inheritance-b661ba897c67


croc.New()
croc.SetX().... Set parameters
croc.Send(file)
croc.Receive()