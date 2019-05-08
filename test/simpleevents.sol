 pragma solidity >=0.5.2 <0.6.0;
/**
  * @title Simple Storage with events
  * @dev Read and write values to the chain
  */
contract SimpleEvents {
  int64 public storedI;
  string public storedS;

  event Changed (
    address indexed from,
    int64 indexed i,
    string indexed s,
    bytes32 h,
    string m
  );

  /**
    * @dev Constructor sets the default value
    * @param i The initial value integer
    * @param s The initial value string
    */
  constructor(int64 i, string memory s) public {
    set(i,s);
  }

  /**
    * @dev Set the value
    * @param i The new value integer
    * @param s The new value string
    */
  function set(int64 i, string memory s) public {
    storedI = i;
    storedS = s;
    emit Changed(msg.sender, i, s, keccak256(bytes(s)), s);
  }

  /**
    * @dev Get the value
    */
  function get() public view returns (int64 i, string memory s) {
    return (storedI, storedS);
  }

}