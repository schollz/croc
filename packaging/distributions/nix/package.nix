{
  lib,
  buildGo127Module,
  buildPackages,
  cacert,
  fetchFromGitHub,
  callPackage,
  installShellFiles,
  makeWrapper,
  nixosTests,
  stdenv,
  versionCheckHook,
}:

buildGo127Module (finalAttrs: {
  pname = "croc";
  version = "11.5.2";

  src = fetchFromGitHub {
    owner = "schollz";
    repo = "croc";
    rev = "v${finalAttrs.version}";
    hash = "sha256-1qUwvMazz3bvkaIEDuI/SeDazINsh1EQgg+vfj/R5kU=";
  };

  vendorHash = "sha256-6B7KZd0Y/RvV9q9U8kHFnKqkDX7uA1tCW2vaFnfeQZ0=";
  patches = [
    ./completions.patch
    ./package-update.patch
  ];

  subPackages = [ "." ];

  nativeBuildInputs = [
    installShellFiles
    makeWrapper
  ];

  postInstall = ''
    export CROC_CONFIG_DIR="$TMPDIR/croc-completion"
    install -Dm644 LICENSE "$out/share/doc/croc/LICENSE"
    install -m644 README.md THIRD_PARTY_NOTICES.md "$out/share/doc/croc/"
    install -m644 src/codephrase/wordlists/LICENSE.txt "$out/share/doc/croc/wordlists-LICENSE.txt"
    installManPage ${./croc.1}
    cp src/install/zsh_autocomplete _croc
    installShellCompletion --cmd croc \
      --bash src/install/bash_autocomplete \
      --fish <(${if stdenv.buildPlatform.canExecute stdenv.hostPlatform then "$out/bin/croc" else lib.getExe (buildPackages.callPackage ./package.nix { })} generate-fish-completion) \
      --zsh _croc
  '';

  postFixup = ''
    wrapProgram "$out/bin/croc" --set-default SSL_CERT_FILE "${cacert}/etc/ssl/certs/ca-bundle.crt"
  '';

  passthru = {
    tests = {
      local-relay = callPackage ./test-local-relay.nix { };
      inherit (nixosTests) croc;
    };
  };

  nativeInstallCheckInputs = [
    versionCheckHook
  ];
  doInstallCheck = true;

  meta = {
    description = "Easily and securely send things from one computer to another";
    longDescription = ''
      Croc is a command line tool written in Go that allows any two computers to
      simply and securely transfer files and folders.

      Croc does all of the following:
      - Allows any two computers to transfer data (using a relay)
      - Provides end-to-end encryption (using PAKE)
      - Enables easy cross-platform transfers (Windows, Linux, Mac)
      - Allows multiple file transfers
      - Allows resuming transfers that are interrupted
      - Does not require a server or port-forwarding
    '';
    homepage = "https://github.com/schollz/croc";
    changelog = "https://github.com/schollz/croc/releases/tag/v${finalAttrs.version}";
    license = lib.licenses.mit;
    maintainers = with lib.maintainers; [
      equirosa
      ryan4yin
      kaynetik
    ];
    mainProgram = "croc";
  };
})
