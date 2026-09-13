{ stdenv, croc }:

stdenv.mkDerivation {
  name = "croc-test-local-relay";

  nativeBuildInputs = [ croc ];

  buildCommand = ''
    HOME="$(mktemp -d)"
    # start a local relay
    croc relay --ports 11111,11112 &
    relay_pid=$!
    trap 'kill "$relay_pid" 2>/dev/null || true; wait "$relay_pid" 2>/dev/null || true' EXIT
    sleep 1
    kill -0 "$relay_pid"

    export CROC_SECRET="sN3nx4hGLeihmn8G"

    # start sender in background
    MSG="See you later, alligator!"
    croc --relay localhost:11111 send --transport relay --no-local --code correct-horse-battery-staple --text "$MSG" &
    sender_pid=$!

    # wait for things to settle
    sleep 1
    MSG2=$(croc --relay localhost:11111 --yes correct-horse-battery-staple)
    wait "$sender_pid"
    kill -0 "$relay_pid"

    # compare
    [ "$MSG" = "$MSG2" ] && touch $out
  '';

  __darwinAllowLocalNetworking = true;

  meta = {
    timeout = 300;
    broken = stdenv.hostPlatform.isDarwin;
  };
}
