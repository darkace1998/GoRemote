import { useAppDispatch, useAppState } from "../../state/store";

export function SearchBox() {
  const { searchQuery } = useAppState();
  const dispatch = useAppDispatch();
  return (
    <div className="search">
      <input
        type="search"
        aria-label="Search connections"
        placeholder="Search by name or tag…"
        value={searchQuery}
        onChange={(e) =>
          dispatch({ type: "search/set", query: e.target.value })
        }
      />
    </div>
  );
}
