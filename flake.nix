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
        qwdtt = final.callPackage ({ buildGoModule, lib }:
          buildGoModule {
            pname = "qwdtt";
            version = "0.9.5";

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
        default = pkgs.qwdtt;
      });

      devShells = eachSystem ({ pkgs, ... }: {
        default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
          ];
        };
      });

      nixosModules = {
        qwdtt = ./modules/nixos;
        default = self.nixosModules.qwdtt;
      };
    };
}
