{
  description = "A simple Go package";

  inputs.nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let

      lastModifiedDate = self.lastModifiedDate or self.lastModified or "19700101";
      version = builtins.substring 0 8 lastModifiedDate;
      supportedSystems = [ "x86_64-linux" "x86_64-darwin" "aarch64-linux" "aarch64-darwin" ];
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
      nixpkgsFor = forAllSystems (system: import nixpkgs { inherit system; });

    in
    {

      packages = forAllSystems (system:
        let
          pkgs = nixpkgsFor.${system};
        in
        {
          chatbang = pkgs.buildGoModule rec {
            pname = "gg";
            inherit version;

            src = ./.;
            vendorHash = null;

            meta = with nixpkgs.lib; {
              description = "CLI tool to access ChatGPT from the terminal without an API key";
              homepage = "https://github.com/ahmedhosssam/chatbang";
              license = licenses.mit;
            };
          };
        });

      devShells = forAllSystems (system:
        let
          pkgs = nixpkgsFor.${system};
        in
        {
          default = pkgs.mkShell {
            buildInputs = with pkgs; [ go gopls gotools go-tools ];
          };
        });

      defaultPackage = forAllSystems (system: self.packages.${system}.chatbang);
    };
}
