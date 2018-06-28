https://medium.com/@simplyianm/why-gos-structs-are-superior-to-class-based-inheritance-b661ba897c67


## Protocol

Every GET/POST request should check the IP address and make sure that there are never more than 2 IP addresses using a single channel. Once two IP addresses are in, then the channel is *full*.

1. **Sender** requests new channel and receives empty channel from **Relay**, or obtains the channel they request (or an error if it is already occupied).

    POST /open
    (optional)
    {
        "channel": "...", // optional
        "role": "sender",
        "success": true,
        "message": "got channel"
    }
    Returns:
    {
        "channel": "...",
        "uuid": "...",
        "success": true,
        "message": "got channel"
    }

2. **Sender** generates *X* using PAKE from secret *pw*.

3. **Sender** sends *X* to **Relay** and the type of curve being used. Returns error if channel is already occupied by sender, otherwise it uses it.

    POST /channel/:channel
    {
        "uuid": "...",
        "x": "...",
        "curve": "p557"
    }
    Returns:
    {
        "success": true,
        "message": "updated x, curve"
    }


4. **Sender** communicates channel + secret *pw* to **Recipient** (human interaction).

5. **Recipient** connects to channel and receives UUID.

5. **Recipient** requests *X* from **Relay** using the channel. Returns error if it doesn't exist yet.


    GET /channel/:channel
    Returns:
    {
        ... all information
        "success": true,
        "message": "updated x"
    }

6. **Recipient** generates *Y*, session key *k_B*, and hashed session key *H(k_B)* using PAKE from secret *pw*.

7. **Recipient** sends *Y*, *H(H(k_B))* to **Relay**.

    ```
    POST /channel/:channel

    {
        "uuid": "...",
        "y": "...",
        "hh_k": "..."
    }
    Returns:
    {
        "success": true,
        "message": "updated y"
    }
    ```
7. **Sender** requests *Y*, *H(H(k_B))* from **Relay**.

    ```
    GET /sender/:channel/y

    Returns:
    {
        "y": "...",
        "hh_k": "...",
        "success": true,
        "message": "got y"
    }
    ```
8. **Sender** uses *Y* to generate its session key *k_A* and *H(k_A)*, and checks *H(H(k_A))*==*H(H(k_B))*. **Sender** aborts here if it is incorrect.

9. **Sender** gives the **Relay** authentication *H(k_A)*.

    ```
    POST /sender/:channel/h_k
    {
        "h_k": "..."
    }

    Returns:
    {
        "success": true,
        "message": "updated h_k"
    }
    ```
10. **Recipient** requests *H(k_A)* from relay and checks against its own. If it doesn't match, then bail.

    ```
    GET /recipient/:channel/h_k

    Returns:
    {
        "h_k": "...",
        "success": true,
        "message": "got h_k"
    }
    ```
11. **Sender** requests that **Relay** creates open TCP connection with itself as sender, identified by *H(k_A)*.

    ```
    GET /sender/:channel/open

    Returns:
    {
        "success": true,
        "message": "opened channel"
    }
    ```
12. **Sender** encrypts data with *k*.

13. **Recipient** requests that **Relay** creates open TCP connection with itself as recipient, identified by *H(k_B)*. 

    ```
    GET /recipient/:channel/open

    Returns:
    {
        "success": true,
        "message": "opened channel"
    }
    ```
this will save the IP address as the reciever
14. **Recipient** starts listening to Relay. (Relay accepts **Recipient** because it knows **Recipient**'s IP address).

15. **Relay**, when it has a sender and recipient identified for TCP connections, staples the connections together. 

16. **Sender** asks **Relay** whether the recipient is ready and connections are stapled.

    ```
    GET /sender/:channel/isready

    Returns:
    {
        "ready": true,
        "success": true,
        "message": "is ready"
    }
    ```
17. **Sender** sends data over TCP.

18. **Recipient** closes relay when finished. Anyone participating in the channel can close the relay at any time. Any of the routes except the first ones will return errors if stuff doesn't exist.
    ```
    GET /close/:channel

    Returns:
    {
        "success": true,
        "message": "closed"
    }
    ```




# Notes

https://play.golang.org/p/1_dfm6us8Nx

https://git.tws.website/t/thesis

https://github.com/tscholl2/siec

*croc* as a library

- use functional options
- every GET/POST request should check the IP address and make sure that there are never more than 2 IP addresses using a single channel

croc.New()
croc.SetX().... Set parameters
croc.Send(file)
croc.Receive()