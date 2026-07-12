{
  description = "qWDTT CLI - VPN client через TURN-серверы VK";

  inputs = { };

  outputs = { self }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];

      eachSystem = f: builtins.listToAttrs (
        builtins.map (system: {
          name = system;
          value = f {
            pkgs = import <nixpkgs> { inherit system; };
            inherit system;
          };
        }) systems
      );

    in
    {
      overlays.default = final: prev: {
        qwdtt-cli = final.callPackage ({ buildGoModule, lib }:
          buildGoModule {
            pname = "qwdtt-cli";
            version = "0.0.2";

            src = ./.;
            vendorHash = null;

            subPackages = [ "." ];
            ldflags = [ "-s" "-w" ];

            meta = with lib; {
              description = "VPN client через TURN-серверы VK";
              license = licenses.gpl3;
              maintainers = [ ];
            };
          }
        ) { };
      };

      packages = eachSystem ({ pkgs, ... }: {
        default = pkgs.qwdtt-cli;
      });

      devShells = eachSystem ({ pkgs, ... }: {
        default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            wireguard-tools
            iproute2
          ];
        };
      });

      nixosModules = {
        qwdtt-cli = ./modules/nixos;
        default = self.nixosModules.qwdtt-cli;
      };
    };
}
