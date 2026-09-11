import { useEffect, useState } from "react";
import { api } from "../lib/api";

export function YamlView({ path }: { path: string }) {
  const [yaml, setYaml] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .swarmYAML(path)
      .then(setYaml)
      .catch((e) => setError(String(e)));
  }, [path]);

  if (error) return <div className="p-4 text-sm text-danger">{error}</div>;

  return (
    <pre className="h-full overflow-auto px-4 py-3 font-mono text-[12.5px] leading-relaxed text-ink">
      {yaml ?? "loading…"}
    </pre>
  );
}
