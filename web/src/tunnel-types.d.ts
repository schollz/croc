declare module "css-tree" {
  export type CssNode = {
    type: string;
    name: string;
    value: string;
    prelude?: { type: string; children: { first?: CssNode } };
  };
  export function parse(source: string, options?: { context: string }): CssNode;
  export function walk(node: CssNode, callback: (node: CssNode) => void): void;
  export function generate(node: CssNode): string;
}
