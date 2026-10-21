import { Icon } from './Icon';

/** The Finuance logo (legacy .logo): a purple gradient tile with the book-with-$ icon. */
export function Logo() {
  return (
    <div className="logo" aria-hidden="true">
      <Icon name="book" />
    </div>
  );
}
