import i18n from "@/lib/i18n";
// customSelectOption returns the temporary "custom" option for a Select
// whose current value is not among its fixed options, or null when the
// current value is already listed (or empty).
//
// The temporary option exists only for the record being edited and only
// while it is the current value: once the user picks a standard option,
// the current value changes and this returns null, so the temporary
// option disappears. It is never added to the fixed option list.
// Display only: saving still writes the state value verbatim, so an
// untouched custom value is preserved.
export function customSelectOption(
  fixedOptions: readonly string[],
  currentValue: string,
): { value: string; label: string } | null {
  if (currentValue === "" || fixedOptions.includes(currentValue)) {
    return null;
  }
  return { value: currentValue, label: i18n.t('common.k009', { currentValue: currentValue }) };
}
